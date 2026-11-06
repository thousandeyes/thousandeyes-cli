// Copyright 2026 Cisco Systems, Inc. and its affiliates
// SPDX-License-Identifier: Apache-2.0

package apicmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/thousandeyes/thousandeyes-cli/internal/config"
	"github.com/thousandeyes/thousandeyes-cli/internal/teapi"
	"github.com/thousandeyes/thousandeyes-cli/internal/testutil/httptransport"
)

func executeAutomationCommand(t *testing.T, args []string, input string, status int, headers http.Header, response string) (capturedRequest, string, error) {
	t.Helper()
	var captured capturedRequest
	restore := teapi.SetRawHTTPClientFactoryForTesting(func() *http.Client {
		return &http.Client{Transport: httptransport.RoundTripFunc(func(request *http.Request) (*http.Response, error) {
			var body []byte
			if request.Body != nil {
				body, _ = io.ReadAll(request.Body)
			}
			captured = capturedRequest{Method: request.Method, URL: request.URL.String(), Header: request.Header.Clone(), Body: body}
			return &http.Response{StatusCode: status, Header: headers.Clone(), Body: io.NopCloser(strings.NewReader(response))}, nil
		})}
	})
	t.Cleanup(restore)
	root := &cobra.Command{Use: "thousandeyes", SilenceUsage: true, SilenceErrors: true}
	RegisterRootResourceCommands(root, func() config.Config { return testConfigProvider() })
	var output bytes.Buffer
	root.SetIn(strings.NewReader(input))
	root.SetOut(&output)
	root.SetErr(io.Discard)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return captured, output.String(), err
}

func TestAutomationBodyStdinPreservesCompletePayload(t *testing.T) {
	const payload = `{"server":"target.invalid","agents":[{"agentId":"6"}],"protocol":"tcp","port":443,"custom":null,"large":9007199254740993}`
	request, output, err := executeAutomationCommand(t, []string{"tests", "agent-to-server", "instant", "--body-file", "-", "--aid", "123", "--expand", "agent", "--http-response"}, payload, 201, http.Header{}, `{"testId":"101"}`)
	if err != nil {
		t.Fatal(err)
	}
	if string(request.Body) != payload || request.URL != "https://api.thousandeyes.com/v7/tests/agent-to-server/instant?aid=123&expand=agent" {
		t.Fatalf("payload or request mapping changed: request=%+v output=%s", request, output)
	}
	var envelope struct {
		StatusCode int             `json:"statusCode"`
		Body       json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil || envelope.StatusCode != 201 || string(envelope.Body) != `{"testId":"101"}` {
		t.Fatalf("invalid response envelope: %s, error=%v", output, err)
	}
}

func TestAutomationHTTPErrorPreservesStatusHeadersAndBody(t *testing.T) {
	for _, status := range []int{404, 429} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			_, output, err := executeAutomationCommand(t, []string{"test-results", "http-server", "get", "--test-id", "101", "--http-response"}, "", status, http.Header{"Retry-After": {"1"}, "X-Organization-Rate-Limit-Reset": {"1793000000"}}, `{"counter":9007199254740993}`)
			if err == nil {
				t.Fatal("HTTP errors must remain command failures")
			}
			var envelope struct {
				StatusCode int             `json:"statusCode"`
				Headers    http.Header     `json:"headers"`
				Body       json.RawMessage `json:"body"`
			}
			if decodeErr := json.Unmarshal([]byte(output), &envelope); decodeErr != nil || envelope.StatusCode != status || envelope.Headers.Get("Retry-After") != "1" || !bytes.Contains(envelope.Body, []byte("9007199254740993")) {
				t.Fatalf("HTTP error metadata lost: output=%s error=%v", output, decodeErr)
			}
			if strings.Contains(err.Error(), "counter") {
				t.Fatal("machine HTTP errors must not echo the raw body on stderr")
			}
		})
	}
}

func TestAutomationRejectsAmbiguousOrInvalidBody(t *testing.T) {
	for _, item := range []struct {
		name, body string
		args       []string
	}{
		{"mixed flags", `{}`, []string{"--url", "https://target.invalid"}},
		{"invalid JSON", `{"private-secret":`, nil},
		{"multiple JSON values", `{} {}`, nil},
	} {
		t.Run(item.name, func(t *testing.T) {
			args := append([]string{"tests", "http-server", "instant", "--body-file", "-", "--http-response"}, item.args...)
			request, output, err := executeAutomationCommand(t, args, item.body, 200, nil, `{}`)
			if err == nil || request.Method != "" || output != "" || strings.Contains(err.Error(), "private-secret") {
				t.Fatalf("invalid input reached API or escaped error: request=%+v output=%q error=%v", request, output, err)
			}
		})
	}
}

func TestJSONOutputPreservesLargeNumbers(t *testing.T) {
	_, output, err := executeAutomationCommand(t, []string{"tests", "list", "--json"}, "", 200, http.Header{"Content-Type": {"application/json"}}, `{"id":9007199254740993}`)
	if err != nil || !strings.Contains(output, "9007199254740993") {
		t.Fatalf("numeric precision lost: %s, error=%v", output, err)
	}
}

func TestGeneratedJSONBodyPreservesLargeNumbers(t *testing.T) {
	request, _, err := executeAutomationCommand(t, []string{"tests", "agent-to-server", "instant", "--server", "target.invalid", "--agents", `[{"agentId":"6","metadata":9007199254740993}]`, "--fixed-packet-rate", "9007199254740993"}, "", 200, nil, `{}`)
	if err != nil || bytes.Count(request.Body, []byte("9007199254740993")) != 2 {
		t.Fatalf("generated JSON numbers changed: %s, error=%v", request.Body, err)
	}
}

func TestGeneratedNumberRejectsEmptyValue(t *testing.T) {
	request, _, err := executeAutomationCommand(t, []string{"tests", "agent-to-server", "instant", "--server", "target.invalid", "--agents", `[{"agentId":"6"}]`, "--fixed-packet-rate", ""}, "", 200, nil, `{}`)
	if err == nil || request.Method != "" {
		t.Fatal("empty numeric input must not become zero")
	}
}

func TestHTTPResponseEnvelopeDoesNotExpandJSONBody(t *testing.T) {
	body := []byte(`{"html":"` + strings.Repeat("<&>", 1024) + `"}`)
	var output bytes.Buffer
	if err := writeHTTPResponse(&output, &teapi.RawResponse{StatusCode: 200, Body: body}); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Body json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil || len(envelope.Body) > len(body) {
		t.Fatalf("envelope expanded JSON beyond the HTTP body limit: received=%d encoded=%d error=%v", len(body), len(envelope.Body), err)
	}
}
