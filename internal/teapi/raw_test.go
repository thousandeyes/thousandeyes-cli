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
	"io"
	"net/http"
	"testing"

	"github.com/thousandeyes/thousandeyes-cli/internal/testutil/httptransport"
	"github.com/thousandeyes/thousandeyes-cli/internal/version"
)

func TestRawClientSendsCLIUserAgent(t *testing.T) {
	previousVersion := version.Version
	version.Version = "v1.2.3"
	t.Cleanup(func() {
		version.Version = previousVersion
	})

	var gotUserAgent string
	restoreClient := SetRawHTTPClientFactoryForTesting(func() *http.Client {
		return &http.Client{
			Transport: httptransport.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
				gotUserAgent = req.Header.Get("User-Agent")
				return &http.Response{
					StatusCode: http.StatusNoContent,
					Header:     http.Header{},
					Body:       io.NopCloser(bytes.NewBuffer(nil)),
				}, nil
			}),
		}
	})
	t.Cleanup(restoreClient)

	client := NewRawClient("https://api.thousandeyes.com/v7", "token")
	if _, err := client.Do(context.Background(), http.MethodGet, "/alerts", nil, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}

	if gotUserAgent != "ThousandEyesCLI/v1.2.3" {
		t.Fatalf("User-Agent: got %q want %q", gotUserAgent, "ThousandEyesCLI/v1.2.3")
	}
}
