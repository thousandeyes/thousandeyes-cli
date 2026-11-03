// Copyright 2026 Cisco Systems, Inc. and its affiliates
// SPDX-License-Identifier: Apache-2.0

package synthetics

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type successfulContextBody struct {
	reader     *strings.Reader
	beforeRead func()
	read       atomic.Bool
	closed     atomic.Bool
}

func (body *successfulContextBody) Read(data []byte) (int, error) {
	if body.beforeRead != nil {
		body.beforeRead()
		body.beforeRead = nil
	}
	n, err := body.reader.Read(data)
	if n > 0 && (err == nil || errors.Is(err, io.EOF)) {
		body.read.Store(true)
	}
	return n, err
}

func (body *successfulContextBody) Close() error {
	body.closed.Store(true)
	return nil
}

func TestRunRejectsSuccessfulFinalReadAfterContextEnds(t *testing.T) {
	for _, item := range []struct {
		name, status string
		cause        error
		deadline     bool
	}{
		{"cancellation", "error", context.Canceled, false},
		{"deadline", "timed-out", context.DeadlineExceeded, true},
	} {
		for _, rule := range []string{"blocking", "non-blocking"} {
			t.Run(item.name+"/"+rule, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				if item.deadline {
					cancel()
					ctx, cancel = context.WithTimeout(context.Background(), 200*time.Millisecond)
				}
				defer cancel()
				body := &successfulContextBody{
					reader: strings.NewReader(`{"test":{"testId":"101"},"results":[{"agent":{"agentId":"6"},"roundId":1793268000,"responseCode":200}]}`),
					beforeRead: func() {
						if !item.deadline {
							cancel()
						}
						<-ctx.Done()
					},
				}
				oldTransport := http.DefaultTransport
				http.DefaultTransport = bodyReadTransport(func(request *http.Request) (*http.Response, error) {
					var responseBody io.ReadCloser = body
					if request.Method == http.MethodPost {
						responseBody = io.NopCloser(strings.NewReader(`{"testId":"101","type":"http-server","agents":[{"agentId":"6","agentType":"cloud"}]}`))
					}
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: responseBody}, nil
				})
				t.Cleanup(func() { http.DefaultTransport = oldTransport })
				config := bodyReadConfig()
				config.Tests[0].ExecutionRule = rule
				report, err := Run(ctx, config, Options{BaseURL: "https://api.example.invalid", Token: "review-fake-token"})
				if !body.read.Load() || !body.closed.Load() {
					t.Fatal("regression must exercise a successful, closed final response read")
				}
				if !errors.Is(err, item.cause) || !report.Failed() {
					t.Fatalf("interruption must fail the gate: error=%v report=%+v", err, report)
				}
				if len(report.Tests) != 1 || report.Tests[0].Status != item.status {
					t.Fatalf("interruption lost from JSON report data: %+v", report)
				}
				var output bytes.Buffer
				if err := WriteJUnit(&output, report); err != nil {
					t.Fatal(err)
				}
				var suite junitSuite
				if err := xml.Unmarshal(output.Bytes(), &suite); err != nil {
					t.Fatal(err)
				}
				if suite.Errors != 1 || suite.Failures != 0 || len(suite.Cases) == 0 {
					t.Fatalf("interruption must remain an execution error in JUnit: %s", output.String())
				}
			})
		}
	}
}
