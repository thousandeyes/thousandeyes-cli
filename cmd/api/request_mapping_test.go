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

package apicmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/spf13/cobra"
	"github.com/thousandeyes/thousandeyes-cli/internal/config"
	"github.com/thousandeyes/thousandeyes-cli/internal/teapi"
	"github.com/thousandeyes/thousandeyes-cli/internal/testutil/httptransport"
)

type capturedRequest struct {
	Method string
	URL    string
	Header http.Header
	Body   []byte
}

const (
	apiContentTypeHeader = "Content-Type"
	methodGotWantFmt     = "method: got %q want %q"
	urlGotFmt            = "url: got %q"
	httpNoContentOut     = "HTTP 204\n"
	stdoutGotWantFmt     = "stdout: got %q want %q"
)

func executeAPICmdWithCapture(t *testing.T, args []string, responseStatus int, responseBody string, responseHeaders http.Header) (capturedRequest, string) {
	t.Helper()

	var captured capturedRequest
	restoreClient := teapi.SetRawHTTPClientFactoryForTesting(func() *http.Client {
		return &http.Client{
			Transport: httptransport.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
				var body []byte
				if req.Body != nil {
					var err error
					body, err = io.ReadAll(req.Body)
					if err != nil {
						t.Fatalf("ReadAll(req.Body): %v", err)
					}
				}
				captured = capturedRequest{
					Method: req.Method,
					URL:    req.URL.String(),
					Header: req.Header.Clone(),
					Body:   body,
				}
				return &http.Response{
					StatusCode: responseStatus,
					Header:     responseHeaders.Clone(),
					Body:       io.NopCloser(bytes.NewBufferString(responseBody)),
				}, nil
			}),
		}
	})
	t.Cleanup(restoreClient)

	cmd := &cobra.Command{Use: "thousandeyes"}
	RegisterRootResourceCommands(cmd, func() config.Config { return testConfigProvider() })
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext(%v): %v", args, err)
	}

	return captured, stdout.String()
}

func TestAPIRequestMappingShorthandRequestBuildsPathAndBody(t *testing.T) {
	got, stdout := executeAPICmdWithCapture(t,
		[]string{
			"account-groups", "create",
			"--expand", "details",
			"--account-group-name", "from-flag",
			"--agents", `["105","719"]`,
		},
		http.StatusNoContent,
		"",
		http.Header{},
	)

	if got.Method != http.MethodPost {
		t.Fatalf(methodGotWantFmt, got.Method, http.MethodPost)
	}
	if got.URL != "https://api.thousandeyes.com/v7/account-groups?expand=details" {
		t.Fatalf(urlGotFmt, got.URL)
	}
	if got.Header.Get(apiContentTypeHeader) != "application/json" {
		t.Fatalf("Content-Type: got %q want application/json", got.Header.Get(apiContentTypeHeader))
	}

	var payload map[string]any
	if err := json.Unmarshal(got.Body, &payload); err != nil {
		t.Fatalf("json.Unmarshal(body): %v", err)
	}
	if payload["accountGroupName"] != "from-flag" {
		t.Fatalf("accountGroupName: got %#v want from-flag", payload["accountGroupName"])
	}
	agents, ok := payload["agents"].([]any)
	if !ok || len(agents) != 2 {
		t.Fatalf("agents: got %#v", payload["agents"])
	}
	if agents[0] != "105" || agents[1] != "719" {
		t.Fatalf("agents: got %#v want [105 719]", agents)
	}
	if stdout != httpNoContentOut {
		t.Fatalf(stdoutGotWantFmt, stdout, httpNoContentOut)
	}
}

func TestAPIRequestMappingNestedShorthandPath(t *testing.T) {
	got, stdout := executeAPICmdWithCapture(t,
		[]string{
			"quotas", "account-groups", "assign",
		},
		http.StatusNoContent,
		"",
		http.Header{},
	)

	if got.Method != http.MethodPost {
		t.Fatalf(methodGotWantFmt, got.Method, http.MethodPost)
	}
	if got.URL != "https://api.thousandeyes.com/v7/quotas/account-groups/assign" {
		t.Fatalf(urlGotFmt, got.URL)
	}
	if got.Header.Get(apiContentTypeHeader) != "" {
		t.Fatalf("Content-Type: got %q want empty", got.Header.Get(apiContentTypeHeader))
	}
	if len(got.Body) != 0 {
		t.Fatalf("body: got %q want empty", string(got.Body))
	}
	if stdout != httpNoContentOut {
		t.Fatalf(stdoutGotWantFmt, stdout, httpNoContentOut)
	}
}

func TestAPIRequestMappingShorthandOperationParamFlags(t *testing.T) {
	got, stdout := executeAPICmdWithCapture(t,
		[]string{
			"alerts", "get",
			"--alert-id", "2783",
			"--aid", "1234",
		},
		http.StatusOK,
		`{"status":"ok"}`,
		http.Header{apiContentTypeHeader: {"application/json"}},
	)

	if got.Method != http.MethodGet {
		t.Fatalf(methodGotWantFmt, got.Method, http.MethodGet)
	}
	if got.URL != "https://api.thousandeyes.com/v7/alerts/2783?aid=1234" {
		t.Fatalf(urlGotFmt, got.URL)
	}
	if stdout != "status: ok\n" {
		t.Fatalf("stdout: got %q want %q", stdout, "status: ok\n")
	}
}

func TestAPIRequestMappingSingleOperationBodyFlagTypes(t *testing.T) {
	bindings := testBodyFieldBindings()
	cmd := newSingleOperationCommand(t, bindings, []string{
		"--enabled", "true",
		"--threshold", "12.5",
		"--filters", `{"regions":["us-east-1"]}`,
	})
	captured := executeSingleOperationWithCapture(t, cmd, testConfigProvider(), testSingleOperation(), bindings)

	assertCapturedSingleOperation(t, captured)
}

func testBodyFieldBindings() []bodyFieldBinding {
	return []bodyFieldBinding{
		{JSONKey: "enabled", FlagName: "enabled", Kind: "bool"},
		{JSONKey: "threshold", FlagName: "threshold", Kind: "number"},
		{JSONKey: "filters", FlagName: "filters", Kind: "json"},
	}
}

func testSingleOperation() apiOperation {
	return apiOperation{
		ID:     "createSynthetic",
		Method: http.MethodPost,
		Path:   "/synthetic",
	}
}

func executeSingleOperationWithCapture(t *testing.T, cmd *cobra.Command, cfg config.Config, op apiOperation, bindings []bodyFieldBinding) capturedRequest {
	t.Helper()

	var captured capturedRequest
	restoreClient := teapi.SetRawHTTPClientFactoryForTesting(func() *http.Client {
		return &http.Client{
			Transport: httptransport.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
				var body []byte
				if req.Body != nil {
					var err error
					body, err = io.ReadAll(req.Body)
					if err != nil {
						t.Fatalf("ReadAll(req.Body): %v", err)
					}
				}
				captured = capturedRequest{
					Method: req.Method,
					URL:    req.URL.String(),
					Header: req.Header.Clone(),
					Body:   body,
				}
				return &http.Response{
					StatusCode: http.StatusNoContent,
					Header:     http.Header{},
					Body:       io.NopCloser(bytes.NewBuffer(nil)),
				}, nil
			}),
		}
	})
	t.Cleanup(restoreClient)

	if err := runAPISingleOperation(cmd, cfg, op, nil, bindings, nil); err != nil {
		t.Fatalf("runAPISingleOperation: %v", err)
	}
	return captured
}

func assertCapturedSingleOperation(t *testing.T, captured capturedRequest) {
	t.Helper()
	if captured.Method != http.MethodPost {
		t.Fatalf(methodGotWantFmt, captured.Method, http.MethodPost)
	}
	if captured.URL != "https://api.thousandeyes.com/v7/synthetic" {
		t.Fatalf("url: got %q want %q", captured.URL, "https://api.thousandeyes.com/v7/synthetic")
	}

	var payload map[string]any
	if err := json.Unmarshal(captured.Body, &payload); err != nil {
		t.Fatalf("json.Unmarshal(body): %v", err)
	}
	if payload["enabled"] != true {
		t.Fatalf("enabled: got %#v want true", payload["enabled"])
	}
	if payload["threshold"] != 12.5 {
		t.Fatalf("threshold: got %#v want 12.5", payload["threshold"])
	}
	filters, ok := payload["filters"].(map[string]any)
	if !ok {
		t.Fatalf("filters: got %#v", payload["filters"])
	}
	regions, ok := filters["regions"].([]any)
	if !ok || len(regions) != 1 || regions[0] != "us-east-1" {
		t.Fatalf("filters.regions: got %#v", filters["regions"])
	}
}

func newSingleOperationCommand(t *testing.T, bindings []bodyFieldBinding, args []string) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.Flags().Bool("json", false, "")
	cmd.Flags().StringArray("path-param", nil, "")
	registerBodyFieldFlags(cmd, bindings)

	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	return cmd
}
