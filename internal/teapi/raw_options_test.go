// Copyright 2026 Cisco Systems, Inc. and its affiliates
// SPDX-License-Identifier: Apache-2.0

package teapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/thousandeyes/thousandeyes-cli/internal/testutil/httptransport"
)

type boundedResponseBody struct {
	read, maximum int
	cancel        context.CancelFunc
	closed        bool
}

func (body *boundedResponseBody) Read(data []byte) (int, error) {
	if body.cancel != nil {
		body.cancel()
		body.cancel = nil
	}
	if body.read >= body.maximum {
		return 0, io.EOF
	}
	count := min(len(data), body.maximum-body.read)
	for index := range data[:count] {
		data[index] = 'x'
	}
	body.read += count
	return count, nil
}
func (body *boundedResponseBody) Close() error { body.closed = true; return nil }

func TestRawResponseLimitBoundsReadsAndClosesBody(t *testing.T) {
	body := &boundedResponseBody{maximum: 100000000}
	restore := SetRawHTTPClientFactoryForTesting(func() *http.Client {
		return &http.Client{Transport: httptransport.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body}, nil
		})}
	})
	defer restore()
	response, err := NewRawClient("https://example.invalid/v7", "fake-token", RawClientOptions{MaxResponseBytes: 4}).Do(context.Background(), http.MethodGet, "/tests", nil, nil, nil)
	if err == nil || response != nil || body.read != 5 || !body.closed {
		t.Fatalf("limit did not bound allocation: reads=%d closed=%v response=%v error=%v", body.read, body.closed, response, err)
	}
}

func TestRawSuccessfulReadDoesNotHideCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := &boundedResponseBody{maximum: 3, cancel: cancel}
	restore := SetRawHTTPClientFactoryForTesting(func() *http.Client {
		return &http.Client{Transport: httptransport.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body}, nil
		})}
	})
	defer restore()
	response, err := NewRawClient("https://example.invalid/v7", "fake-token").Do(ctx, http.MethodGet, "/tests", nil, nil, nil)
	if !errors.Is(err, context.Canceled) || response != nil || !body.closed {
		t.Fatalf("canceled response accepted: response=%v error=%v closed=%v", response, err, body.closed)
	}
}

func TestRawHTTPErrorRetainsResponseHeaders(t *testing.T) {
	restore := SetRawHTTPClientFactoryForTesting(func() *http.Client {
		return &http.Client{Transport: httptransport.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {"3"}}, Body: io.NopCloser(strings.NewReader(`{"error":"limited"}`))}, nil
		})}
	})
	defer restore()
	response, err := NewRawClient("https://example.invalid/v7", "fake-token").Do(context.Background(), http.MethodGet, "/tests", nil, nil, nil)
	var apiError *APIError
	if !errors.As(err, &apiError) || response == nil || response.Header.Get("Retry-After") != "3" {
		t.Fatalf("HTTP error metadata lost: response=%v error=%v", response, err)
	}
}
