// Copyright 2026 Cisco Systems, Inc. and its affiliates
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package teapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/thousandeyes/thousandeyes-cli/internal/version"
)

type RawClient struct {
	baseURL          string
	token            string
	client           *http.Client
	maxResponseBytes int64
}

type RawClientOptions struct {
	NoRedirects      bool
	MaxResponseBytes int64
}

var rawHTTPClientFactory = func() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
	}
}

func NewRawClient(baseURL, token string, options ...RawClientOptions) *RawClient {
	var settings RawClientOptions
	if len(options) > 0 {
		settings = options[0]
	}
	client := rawHTTPClientFactory()
	if settings.NoRedirects {
		copy := *client
		copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client = &copy
	}
	return &RawClient{
		baseURL:          strings.TrimRight(baseURL, "/"),
		token:            token,
		client:           client,
		maxResponseBytes: settings.MaxResponseBytes,
	}
}

func SetRawHTTPClientFactoryForTesting(factory func() *http.Client) func() {
	prev := rawHTTPClientFactory
	if factory == nil {
		return func() {
			rawHTTPClientFactory = prev
		}
	}
	rawHTTPClientFactory = factory
	return func() {
		rawHTTPClientFactory = prev
	}
}

type RawResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

func (c *RawClient) Get(ctx context.Context, path string, query url.Values, out any) error {
	resp, err := c.Do(ctx, http.MethodGet, path, query, nil, nil)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(resp.Body, out); err != nil {
		return fmt.Errorf("decode response body: %w", err)
	}
	return nil
}

func (c *RawClient) Do(ctx context.Context, method, path string, query url.Values, headers http.Header, body []byte) (*RawResponse, error) {
	u := c.resolveURL(path)
	if encoded := query.Encode(); encoded != "" {
		u += "?" + encoded
	}

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Accept", "application/hal+json, application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", version.UserAgent())
	for key, values := range headers {
		req.Header.Del(key)
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()

	var reader io.Reader = resp.Body
	if c.maxResponseBytes > 0 {
		limit := c.maxResponseBytes
		if limit < math.MaxInt64 {
			limit++
		}
		reader = io.LimitReader(reader, limit)
	}
	respBody, err := io.ReadAll(reader)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if c.maxResponseBytes > 0 && int64(len(respBody)) > c.maxResponseBytes {
		return nil, fmt.Errorf("response body exceeded maximum of %d bytes", c.maxResponseBytes)
	}
	result := &RawResponse{StatusCode: resp.StatusCode, Header: resp.Header.Clone(), Body: respBody}

	if resp.StatusCode >= 300 {
		return result, &APIError{StatusCode: resp.StatusCode, Body: respBody}
	}
	return result, nil
}

func (c *RawClient) resolveURL(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if strings.HasPrefix(path, "/") {
		return c.baseURL + path
	}
	return c.baseURL + "/" + path
}

type APIError struct {
	StatusCode int
	Body       []byte
}

func (e *APIError) Error() string {
	if len(e.Body) == 0 {
		return fmt.Sprintf("api error %d", e.StatusCode)
	}
	return fmt.Sprintf("api error %d: %s", e.StatusCode, string(e.Body))
}
