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
	"strings"
	"testing"
)

func TestAPIResourceParentHelpOmitsRepeatedGlobalFlags(t *testing.T) {
	cmd := newRootCommand()
	stdout, stderr := captureCommandIO(t, cmd)
	cmd.SetArgs([]string{"tests", "http-server", "-h"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v stderr=%q", err, stderr.String())
	}
	out := stdout.String()
	if strings.Contains(out, "Global Flags:") {
		t.Fatalf("expected compact help without Global Flags section; got:\n%s", out)
	}
	if !strings.Contains(out, "get") || !strings.Contains(out, "List HTTP Server tests") {
		t.Fatalf("expected verb list in help; got:\n%s", out)
	}
	if !strings.Contains(out, "Shared API flags") {
		t.Fatalf("expected single-line shared flags hint; got:\n%s", out)
	}
	if strings.Contains(out, "OpenAPI shorthand for") {
		t.Fatalf("expected no redundant OpenAPI shorthand line; got:\n%s", out)
	}
}

func TestAPIVerbHelpUsesSharedFlagsHint(t *testing.T) {
	cmd := newRootCommand()
	stdout, stderr := captureCommandIO(t, cmd)
	cmd.SetArgs([]string{"tests", "http-server", "get", "-h"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v stderr=%q", err, stderr.String())
	}
	out := stdout.String()
	if strings.Contains(out, "Global Flags:") {
		t.Fatalf("expected verb help without Global Flags block; got:\n%s", out)
	}
	if strings.Contains(out, "API Details:") {
		t.Fatalf("expected compact verb help without API details block; got:\n%s", out)
	}
	if !strings.Contains(out, "Shared API flags") {
		t.Fatalf("expected shared flags hint; got:\n%s", out)
	}
}
