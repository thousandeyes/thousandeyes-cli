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

package cmd

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/thousandeyes/thousandeyes-cli/internal/teapi"
	"github.com/thousandeyes/thousandeyes-cli/internal/testutil/httptransport"
)

const (
	rootTokenFlag        = "--token"
	rootBaseURLFlag      = "--base-url"
	rootExecuteErrFmt    = "command.Execute: %v\nstderr:\n%s"
	rootUnexpectedErrFmt = "unexpected stderr: %q"
)

func captureAPIClientRoundTrip(
	t *testing.T, wantMethod string, wantURL string, wantBearer string,
	statusCode int, responseBody string, responseHeaders http.Header,
) {
	t.Helper()

	restoreClient := teapi.SetRawHTTPClientFactoryForTesting(func() *http.Client {
		return &http.Client{
			Transport: httptransport.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != wantURL {
					t.Fatalf("URL: got %q want %q", r.URL.String(), wantURL)
				}
				if r.Method != wantMethod {
					t.Fatalf("method: got %q want %q", r.Method, wantMethod)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer "+wantBearer {
					t.Fatalf("Authorization header: got %q want %q", got, "Bearer "+wantBearer)
				}
				return &http.Response{
					StatusCode: statusCode,
					Header:     responseHeaders,
					Body:       io.NopCloser(strings.NewReader(responseBody)),
				}, nil
			}),
		}
	})
	t.Cleanup(restoreClient)
}

func newTestRootCommand(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := newRootCommand()
	cmd.SilenceUsage = true
	return cmd
}

// TestAPICommand_Integration_CobraResourceVerb exercises OpenAPI shorthand as resource then verb.
func TestAPICommandIntegrationCobraResourceVerb(t *testing.T) {
	const wantBearer = "integration-test-token"
	captureAPIClientRoundTrip(
		t,
		http.MethodGet,
		"https://example.invalid/v7/tests",
		wantBearer,
		http.StatusOK,
		`{"status":"ok"}`,
		http.Header{"Content-Type": {"application/json"}},
	)

	cmd := newTestRootCommand(t)
	stdout, stderr := captureCommandIO(t, cmd)

	cmd.SetArgs([]string{
		rootTokenFlag, wantBearer,
		rootBaseURLFlag, "https://example.invalid",
		"tests", "list",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf(rootExecuteErrFmt, err, stderr.String())
	}
	if msg := stderr.String(); msg != "" {
		t.Fatalf(rootUnexpectedErrFmt, msg)
	}

	got := stdout.String()
	want := "status: ok\n"
	if got != want {
		t.Fatalf("stdout:\ngot  %q\nwant %q", got, want)
	}
}

// TestAPICommand_Integration_CobraHelp shows root help output for shorthand-only command mode.
func TestAPICommandIntegrationCobraHelp(t *testing.T) {
	cmd := newTestRootCommand(t)
	stdout, stderr := captureCommandIO(t, cmd)

	cmd.SetArgs([]string{
		rootTokenFlag, "help-test-token",
		rootBaseURLFlag, "https://api.thousandeyes.com",
		"--help",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf(rootExecuteErrFmt, err, stderr.String())
	}
	if msg := stderr.String(); msg != "" {
		t.Fatalf(rootUnexpectedErrFmt, msg)
	}

	out := stdout.String()
	if strings.Contains(out, "request <http-method> <path>") {
		t.Fatalf("help output should not include removed request command; got:\n%s", out)
	}
	if strings.Contains(out, "--base-url") {
		t.Fatalf("help output should hide development-only --base-url flag; got:\n%s", out)
	}
}

func TestAPICommandIntegrationReadsBaseURLFromEnvironment(t *testing.T) {
	const wantBearer = "integration-test-token"
	t.Setenv("TE_BASE_URL", "https://example.invalid")
	captureAPIClientRoundTrip(
		t,
		http.MethodGet,
		"https://example.invalid/v7/tests",
		wantBearer,
		http.StatusOK,
		`{"status":"ok"}`,
		http.Header{"Content-Type": {"application/json"}},
	)

	cmd := newTestRootCommand(t)
	stdout, stderr := captureCommandIO(t, cmd)

	cmd.SetArgs([]string{
		rootTokenFlag, wantBearer,
		"tests", "list",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf(rootExecuteErrFmt, err, stderr.String())
	}
	if msg := stderr.String(); msg != "" {
		t.Fatalf(rootUnexpectedErrFmt, msg)
	}

	got := stdout.String()
	want := "status: ok\n"
	if got != want {
		t.Fatalf("stdout:\ngot  %q\nwant %q", got, want)
	}
}

// TestAPICommand_Integration_RequiresToken documents that config loading runs before shorthand command.
// Pass an explicit empty --token so this test does not inherit a token from earlier rootCmd runs
// (Cobra keeps persistent flag values across Execute calls).
func TestAPICommandIntegrationRequiresToken(t *testing.T) {
	t.Setenv("TE_TOKEN", "")
	cmd := newTestRootCommand(t)
	stdout, _ := captureCommandIO(t, cmd)

	cmd.SetArgs([]string{rootTokenFlag, "", "tests", "list"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when token is missing")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Fatalf("expected token-related error, got: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout when SilenceUsage is set, got: %q", stdout.String())
	}
}
