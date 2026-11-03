// Copyright 2026 Cisco Systems, Inc. and its affiliates
// SPDX-License-Identifier: Apache-2.0

package synthetics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
)

func TestRunPreservesNativeConfigurationForBothTypedAdapters(t *testing.T) {
	config := loadTypedSuite(t, `{
		"version":1,"accountGroupId":"123","timeout":"2s","pollInterval":"1ms","tests":[
			{"name":"typed-http","type":"http-server","config":{
				"url":"https://service.invalid/health?literal=keep","testName":"original HTTP label","followRedirects":false,
				"agents":[{"agentId":"6","sourceIpAddress":"192.0.2.10","futureAgentOption":{"enabled":false,"count":0}}],
				"futureOptions":{"large":9007199254740993,"precise":1.234567890123456789,"empty":[],"nothing":null},
				"nativeZero":0,"nativeNull":null
			},"thresholds":{"expectedResponseCodes":[200],"maxResponseTime":0,"maxTotalTime":0}},
			{"name":"typed-network","type":"agent-to-server","config":{
				"server":"service.invalid","port":443,"protocol":"tcp","testName":"original network label","bandwidthMeasurements":false,
				"agents":[{"agentId":"6","sourceIpAddress":"192.0.2.10","futureAgentOption":{"enabled":false,"count":0}}],
				"futureOptions":{"large":9007199254740993,"precise":1.234567890123456789,"empty":[],"nothing":null},
				"nativeZero":0,"nativeNull":null
			},"thresholds":{"maxLoss":0,"maxLatency":0,"maxJitter":0}}
		]
	}`)
	before := make([][]byte, len(config.Tests))
	for index, test := range config.Tests {
		var err error
		before[index], err = json.Marshal(test.Config)
		if err != nil {
			t.Fatal(err)
		}
	}
	var mutex sync.Mutex
	payloads := map[string][]byte{}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		mutex.Lock()
		requests++
		mutex.Unlock()
		if request.Header.Get("Authorization") != "Bearer typed-config-fake-token" || request.URL.Query().Get("aid") != "123" {
			t.Error("request authentication or account identity changed")
		}
		w.Header().Set("Content-Type", "application/json")
		var response any
		switch request.URL.Path {
		case "/v7/tests/http-server/instant", "/v7/tests/agent-to-server/instant":
			if request.Method != http.MethodPost || request.URL.Query().Get("expand") != "agent" {
				t.Error("creation method or expansion changed")
			}
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Error(err)
			}
			mutex.Lock()
			payloads[request.URL.Path] = body
			mutex.Unlock()
			id := "101"
			if request.URL.Path == "/v7/tests/agent-to-server/instant" {
				id = "102"
			}
			w.WriteHeader(http.StatusCreated)
			response = map[string]any{"testId": id, "agents": []any{map[string]any{"agentId": "6", "agentType": "enterprise"}}}
		case "/v7/test-results/101/http-server":
			response = map[string]any{"results": []any{map[string]any{"agent": map[string]any{"agentId": "6"}, "roundId": 1700000000, "responseCode": 200, "responseTime": 0, "totalTime": 0}}}
		case "/v7/test-results/102/network":
			response = map[string]any{"results": []any{map[string]any{"agent": map[string]any{"agentId": "6"}, "roundId": 1700000000, "loss": 0, "avgLatency": 0, "jitter": 0}}}
		default:
			t.Errorf("unexpected request path: %s", request.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	report, err := Run(context.Background(), config, Options{BaseURL: server.URL, Token: "typed-config-fake-token"})
	if err != nil || report.Failed() || len(report.Tests) != 2 {
		t.Fatalf("typed adapters changed execution behavior: report=%+v error=%v", report, err)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if requests != 4 || len(payloads) != 2 {
		t.Fatalf("expected one creation/result per adapter: requests=%d payloads=%d", requests, len(payloads))
	}
	for index, test := range config.Tests {
		got := decodeNativeTypedPayload(t, payloads["/v7/tests/"+test.Type+"/instant"])
		want := decodeNativeTypedPayload(t, before[index])
		want["testName"] = fmt.Sprintf("ci-%s-%d", report.RunID, index+1)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s changed native values beyond testName: got=%v want=%v", test.Type, got, want)
		}
		after, err := json.Marshal(test.Config)
		if err != nil || !bytes.Equal(after, before[index]) {
			t.Errorf("%s mutated the source native configuration: %v", test.Type, err)
		}
		if report.Tests[index].Status != "passed" {
			t.Errorf("%s did not retain its passing zero measurements: %+v", test.Type, report.Tests[index])
		}
	}
}

func decodeNativeTypedPayload(t *testing.T, data []byte) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
