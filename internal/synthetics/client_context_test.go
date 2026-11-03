// Copyright 2026 Cisco Systems, Inc. and its affiliates
// SPDX-License-Identifier: Apache-2.0

package synthetics

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type bodyReadTransport func(*http.Request) (*http.Response, error)

func (transport bodyReadTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

type bodyReadCloser struct {
	read  func() (int, error)
	close func()
}

func (body bodyReadCloser) Read([]byte) (int, error) { return body.read() }
func (body bodyReadCloser) Close() error {
	body.close()
	return nil
}

func TestRunPreservesContextFailureDuringResultBodyRead(t *testing.T) {
	for _, item := range []struct {
		name, status, message string
		deadline              bool
		cause                 error
	}{
		{"deadline", "timed-out", "execution deadline exceeded before all required measurements arrived", true, context.DeadlineExceeded},
		{"cancellation", "error", "execution canceled", false, context.Canceled},
	} {
		t.Run(item.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if item.deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 200*time.Millisecond)
			}
			defer cancel()
			read, closed, creations, results := false, false, 0, 0
			oldTransport := http.DefaultTransport
			http.DefaultTransport = bodyReadTransport(func(request *http.Request) (*http.Response, error) {
				var body io.ReadCloser
				if request.Method == http.MethodPost {
					creations++
					body = io.NopCloser(strings.NewReader(`{"testId":"101","type":"http-server","agents":[{"agentId":"6","agentType":"cloud"}]}`))
				} else {
					results++
					body = bodyReadCloser{read: func() (int, error) {
						read = true
						if !item.deadline {
							cancel()
						}
						<-request.Context().Done()
						return 0, request.Context().Err()
					}, close: func() { closed = true }}
				}
				return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = oldTransport })

			report, err := Run(ctx, bodyReadConfig(), Options{BaseURL: "https://api.example.invalid", Token: "review-fake-token"})
			if !read || !closed || creations != 1 || results != 1 {
				t.Fatalf("expected one creation and one closed result body: read=%v closed=%v creations=%d results=%d", read, closed, creations, results)
			}
			if !errors.Is(err, item.cause) {
				t.Fatalf("body-read context failure lost: error=%v, want %v", err, item.cause)
			}
			if len(report.Tests) != 1 || report.Tests[0].Status != item.status || report.Tests[0].Message != item.message {
				t.Fatalf("wrong test outcome for body-read %s: %+v", item.name, report)
			}
			if agents := report.Tests[0].Agents; len(agents) != 1 || agents[0].Status != item.status || agents[0].Message != item.message {
				t.Fatalf("wrong agent outcome for body-read %s: %+v", item.name, agents)
			}
			if !report.Failed() {
				t.Fatal("interrupted response body must fail the CI gate")
			}
		})
	}
}

func TestRunSanitizesResultBodyReadErrors(t *testing.T) {
	const secret = "review-fake-token-and-private-response-details"
	oldTransport := http.DefaultTransport
	http.DefaultTransport = bodyReadTransport(func(request *http.Request) (*http.Response, error) {
		var body io.ReadCloser
		if request.Method == http.MethodPost {
			body = io.NopCloser(strings.NewReader(`{"testId":"101","type":"http-server","agents":[{"agentId":"6","agentType":"cloud"}]}`))
		} else {
			body = bodyReadCloser{read: func() (int, error) { return 0, errors.New(secret) }, close: func() {}}
		}
		return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	report, err := Run(context.Background(), bodyReadConfig(), Options{BaseURL: "https://api.example.invalid", Token: "review-fake-token"})
	if err == nil || !report.Failed() || len(report.Tests) != 1 || report.Tests[0].Status != "error" {
		t.Fatalf("body I/O failure was lost: report=%+v error=%v", report, err)
	}
	data, marshalErr := json.Marshal(report)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(err.Error()+string(data), secret) {
		t.Fatal("raw body-read error leaked into the error or report")
	}
}

func bodyReadConfig() Config {
	return Config{Version: 1, AccountGroupID: "123", Timeout: "2s", PollInterval: "1ms", Tests: []TestConfig{{
		Name: "body-read-check", Type: "http-server", Config: map[string]json.RawMessage{
			"url": json.RawMessage(`"https://target.invalid"`), "agents": json.RawMessage(`[{"agentId":"6"}]`),
		},
	}}}
}
