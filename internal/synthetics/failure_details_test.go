// Copyright 2026 Cisco Systems, Inc. and its affiliates
// SPDX-License-Identifier: Apache-2.0

package synthetics_test

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/thousandeyes/thousandeyes-cli/internal/synthetics"
)

func TestRunExplainsFailedChecksInEveryReport(t *testing.T) {
	for _, item := range []struct {
		name, kind, thresholds, measurements, message string
	}{
		{"default HTTP codes", "http-server", `{}`, `"responseCode":500`, "HTTP response code 500 did not match expected 200-399"},
		{"selected HTTP codes", "http-server", `{"expectedResponseCodes":[200,204]}`, `"responseCode":503`, "HTTP response code 503 did not match expected [200, 204]"},
		{"response time", "http-server", `{"maxResponseTime":100}`, `"responseCode":200,"responseTime":125.5`, "responseTime 125.5 ms exceeded maximum 100 ms"},
		{"selected zero timing", "http-server", `{"maxResponseTime":0}`, `"responseCode":200,"responseTime":0.5`, "responseTime 0.5 ms exceeded maximum 0 ms"},
		{"total time", "http-server", `{"maxTotalTime":150}`, `"responseCode":200,"totalTime":200`, "totalTime 200 ms exceeded maximum 150 ms"},
		{"default loss", "agent-to-server", `{}`, `"loss":10.5`, "loss 10.5% exceeded maximum 0%"},
		{"selected loss", "agent-to-server", `{"maxLoss":5}`, `"loss":10.5`, "loss 10.5% exceeded maximum 5%"},
		{"latency", "agent-to-server", `{"maxLatency":100}`, `"loss":0,"avgLatency":100.5`, "avgLatency 100.5 ms exceeded maximum 100 ms"},
		{"jitter", "agent-to-server", `{"maxJitter":2}`, `"loss":0,"jitter":3.5`, "jitter 3.5 ms exceeded maximum 2 ms"},
		{"native HTTP error", "http-server", `{"expectedResponseCodes":[200]}`, `"responseCode":503,"errorType":"HTTP","errorDetails":"private-native-detail failure-details-fake-token"`, "agent reported a measurement error; HTTP response code 503 did not match expected [200]"},
		{"native error with accepted code", "http-server", `{"expectedResponseCodes":[200]}`, `"responseCode":200,"errorType":"Receive","errorDetails":"private-native-detail failure-details-fake-token"`, "agent reported a measurement error"},
		{"native error with invalid code", "http-server", `{"expectedResponseCodes":[200]}`, `"responseCode":0,"errorType":"DNS","errorDetails":"private-native-detail failure-details-fake-token"`, "agent reported a measurement error"},
		{"native error", "http-server", `{}`, `"errorType":"DNS","errorDetails":"private-native-detail failure-details-fake-token"`, "agent reported a measurement error"},
	} {
		t.Run(item.name, func(t *testing.T) {
			report := runFailureDetailsSuite(t, item.kind, item.thresholds, item.measurements)
			if !report.Failed() || len(report.Tests) != 1 || report.Tests[0].Status != "failed" || len(report.Tests[0].Agents) != 1 {
				t.Fatalf("failure must retain its blocking result: %+v", report)
			}
			if agent := report.Tests[0].Agents[0]; agent.Status != "failed" || agent.Message != item.message {
				t.Fatalf("failure details: status=%s message=%q, want %q", agent.Status, agent.Message, item.message)
			}

			var output bytes.Buffer
			if err := synthetics.WriteJSON(&output, report); err != nil {
				t.Fatal(err)
			}
			var decoded synthetics.Report
			if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
				t.Fatal(err)
			}
			if got := decoded.Tests[0].Agents[0].Message; got != item.message {
				t.Fatalf("JSON message=%q, want %q", got, item.message)
			}
			assertFailureReportRedaction(t, output.String())

			output.Reset()
			if err := synthetics.WriteSummary(&output, report); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), item.message) {
				t.Fatalf("terminal summary omitted failure details: %s", output.String())
			}
			assertFailureReportRedaction(t, output.String())

			output.Reset()
			if err := synthetics.WriteJUnit(&output, report); err != nil {
				t.Fatal(err)
			}
			var suite struct {
				Failures int `xml:"failures,attr"`
				Cases    []struct {
					Failure struct {
						Message string `xml:"message,attr"`
					} `xml:"failure"`
				} `xml:"testcase"`
			}
			if err := xml.Unmarshal(output.Bytes(), &suite); err != nil {
				t.Fatal(err)
			}
			if suite.Failures != 1 || len(suite.Cases) != 1 || suite.Cases[0].Failure.Message != item.message {
				t.Fatalf("JUnit omitted failure details: %s", output.String())
			}
			assertFailureReportRedaction(t, output.String())
		})
	}
}

func TestRunBoundsExpectedHTTPCodeDetails(t *testing.T) {
	codes := make([]int, 400)
	for index := range codes {
		codes[index] = 100 + index
	}
	thresholds, err := json.Marshal(map[string]any{"expectedResponseCodes": codes})
	if err != nil {
		t.Fatal(err)
	}
	report := runFailureDetailsSuite(t, "http-server", string(thresholds), `"responseCode":503`)
	message := report.Tests[0].Agents[0].Message
	if !strings.Contains(message, "HTTP response code 503") || !strings.Contains(message, "100, 101") || !strings.Contains(message, "390 more") || len(message) > 200 {
		t.Fatalf("large response-code policy needs bounded actual/expected details: %q", message)
	}
}

type failureDetailsTransport func(*http.Request) (*http.Response, error)

func (transport failureDetailsTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func runFailureDetailsSuite(t *testing.T, kind, thresholds, measurements string) synthetics.Report {
	t.Helper()
	const fakeToken = "failure-details-fake-token"
	previousTransport := http.DefaultTransport
	http.DefaultTransport = failureDetailsTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Scheme != "https" || request.URL.Host != "synthetics.example.invalid" {
			t.Fatalf("unexpected API origin: %s", request.URL)
		}
		var body string
		switch request.Method {
		case http.MethodPost:
			if request.URL.Path != "/v7/tests/"+kind+"/instant" || request.URL.Query().Get("expand") != "agent" {
				t.Fatalf("unexpected creation endpoint: %s", request.URL)
			}
			body = `{"testId":"101","type":"` + kind + `","agents":[{"agentId":"6","agentType":"cloud"}]}`
		case http.MethodGet:
			layer := kind
			if kind == "agent-to-server" {
				layer = "network"
			}
			if request.URL.Path != "/v7/test-results/101/"+layer {
				t.Fatalf("unexpected result endpoint: %s", request.URL)
			}
			body = `{"test":{"testId":"101"},"results":[{"agent":{"agentId":"6"},"roundId":1700000000,` + measurements + `}]}`
		default:
			t.Fatalf("unexpected API method: %s", request.Method)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	native := `{"url":"https://target.invalid","agents":[{"agentId":"6"}]}`
	if kind == "agent-to-server" {
		native = `{"server":"target.invalid","protocol":"icmp","agents":[{"agentId":"6"}]}`
	}
	var config synthetics.Config
	payload := fmt.Sprintf(`{"version":1,"accountGroupId":"123","timeout":"1s","pollInterval":"1ms","tests":[{"name":"failed-check","type":%q,"config":%s,"thresholds":%s}]}`, kind, native, thresholds)
	if err := json.Unmarshal([]byte(payload), &config); err != nil {
		t.Fatal(err)
	}
	report, err := synthetics.Run(context.Background(), config, synthetics.Options{BaseURL: "https://synthetics.example.invalid", Token: fakeToken})
	if err != nil {
		t.Fatalf("a failed assertion must remain an evaluated result: %v", err)
	}
	return report
}

func assertFailureReportRedaction(t *testing.T, output string) {
	t.Helper()
	for _, secret := range []string{"private-native-detail", "failure-details-fake-token"} {
		if strings.Contains(output, secret) {
			t.Fatal("failure report included private native details or the API token")
		}
	}
}
