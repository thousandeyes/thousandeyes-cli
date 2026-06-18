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

package apispec

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

const (
	testSpecFilename         = "thousandeyes.yaml"
	testSpecPathsYAML        = "paths:\n"
	pathAlerts               = "/alerts"
	pathAlertByID            = "/alerts/{alertId}"
	pathAgents               = "/agents"
	overlayAlertsListCommand = "alerts/list"
	overlayAgentsListCommand = "agents/list"
	mkdirAllErrFmt           = "MkdirAll: %v"
)

func TestParseOperationIndexCapturesOperationsAndBodies(t *testing.T) {
	t.Parallel()

	raw := []byte(`
paths:
  /alerts:
    get:
      operationId: getAlerts
      summary: List alerts
      description: |-
        first line
        second line
    post:
      operationId: createAlert
      parameters:
        - $ref: '#/components/parameters/AccountGroupId'
        - name: alertId
          in: path
          required: true
          description: |-
            alert id
            path param
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/CreateAlertRequest'
      responses:
        '201':
          description: Created
components:
  parameters:
    AccountGroupId:
      name: aid
      in: query
      required: false
      description: Account group identifier.
  schemas:
    CreateAlertRequest:
      type: object
`)

	got, err := ParseOperationIndex(raw)
	if err != nil {
		t.Fatalf("ParseOperationIndex: %v", err)
	}

	getOp := got["getAlerts"]
	if getOp.Method != http.MethodGet || getOp.Path != pathAlerts {
		t.Fatalf("unexpected get operation: %#v", getOp)
	}
	if getOp.Description != "first line second line" {
		t.Fatalf("description: got %q", getOp.Description)
	}

	postOp := got["createAlert"]
	if !postOp.HasBody {
		t.Fatalf("expected request body: %#v", postOp)
	}
	if len(postOp.RequestBody) != 1 || postOp.RequestBody[0].SchemaRef != "#/components/schemas/CreateAlertRequest" {
		t.Fatalf("unexpected request body: %#v", postOp.RequestBody)
	}
	if len(postOp.Parameters) != 2 {
		t.Fatalf("unexpected parameter count: %#v", postOp.Parameters)
	}
	if postOp.Parameters[0].Name != "aid" || postOp.Parameters[0].In != "query" {
		t.Fatalf("unexpected component param: %#v", postOp.Parameters[0])
	}
	if postOp.Parameters[1].Description != "alert id path param" {
		t.Fatalf("unexpected inline param description: %#v", postOp.Parameters[1])
	}
}

func TestParseOperationIndexCapturesInlineArrayRequestBodySchema(t *testing.T) {
	t.Parallel()

	raw := []byte(`
paths:
  /connectors/generic/{id}/operations:
    put:
      operationId: setGenericConnectorOperations
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: array
              items:
                type: string
      responses:
        '200':
          description: OK
`)

	got, err := ParseOperationIndex(raw)
	if err != nil {
		t.Fatalf("ParseOperationIndex: %v", err)
	}

	op := got["setGenericConnectorOperations"]
	if !op.HasBody {
		t.Fatalf("expected request body: %#v", op)
	}
	if len(op.RequestBody) != 1 {
		t.Fatalf("unexpected request body count: %#v", op.RequestBody)
	}
	if op.RequestBody[0].SchemaRef != "" {
		t.Fatalf("expected no schema ref for inline schema, got: %#v", op.RequestBody[0])
	}
	if op.RequestBody[0].SchemaType != "array" || op.RequestBody[0].ItemsType != "string" {
		t.Fatalf("unexpected inline array schema metadata: %#v", op.RequestBody[0])
	}
}

func TestParseOperationIndexRejectsMissingOperations(t *testing.T) {
	t.Parallel()

	_, err := ParseOperationIndex([]byte(testSpecPathsYAML))
	if err == nil || !strings.Contains(err.Error(), "no operationIds found") {
		t.Fatalf("expected missing operations error, got %v", err)
	}
}

func TestParseComponentParametersAndHelpers(t *testing.T) {
	t.Parallel()

	raw := []byte(`
components:
  parameters:
    AccountGroupId:
      name: aid
      in: query
      required: true
      description: |-
        first line
        second line
    Ignored:
      in: query
paths: {}
`)

	got := parseComponentParameters(raw)
	want := map[string]Parameter{
		"AccountGroupId": {
			Name:        "aid",
			In:          "query",
			Required:    true,
			Description: "first line second line",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseComponentParameters: got %#v want %#v", got, want)
	}

	if componentRefName(`"#/components/parameters/AccountGroupId"`) != "AccountGroupId" {
		t.Fatal("componentRefName did not trim the prefix")
	}
	words := SplitCamelCase("HTTPServerTest")
	if !reflect.DeepEqual(words, []string{"HTTP", "Server", "Test"}) {
		t.Fatalf("splitCamelCase: got %#v", words)
	}
	if !IsUpperASCII('A') || IsUpperASCII('a') {
		t.Fatal("isUpperASCII returned unexpected result")
	}
}

func TestCandidateSpecPathsAndFindSpecPath(t *testing.T) {
	tmp := t.TempDir()
	specDir := filepath.Join(tmp, "nested", "repo", "api")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatalf(mkdirAllErrFmt, err)
	}
	specPath := filepath.Join(specDir, testSpecFilename)
	if err := os.WriteFile(specPath, []byte(testSpecPathsYAML), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	paths := candidateSpecPaths(filepath.Join(tmp, "nested", "repo", "cmd"))
	if paths[0] != filepath.Join(tmp, "nested", "repo", "cmd", "api", testSpecFilename) {
		t.Fatalf("unexpected first candidate: %q", paths[0])
	}

	chdirForTest(t, filepath.Join(tmp, "nested", "repo"))

	got, err := findSpecPath()
	if err != nil {
		t.Fatalf("findSpecPath: %v", err)
	}
	gotEval, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("EvalSymlinks(got): %v", err)
	}
	wantEval, err := filepath.EvalSymlinks(specPath)
	if err != nil {
		t.Fatalf("EvalSymlinks(specPath): %v", err)
	}
	if gotEval != wantEval {
		t.Fatalf("findSpecPath: got %q want %q", gotEval, wantEval)
	}
}

func TestLoadOperationIndexFromTempSpec(t *testing.T) {
	tmp := t.TempDir()
	raw := []byte(`
paths:
  /tests:
    get:
      operationId: getTests
`)
	_ = writeTempSpecFile(t, tmp, raw)
	chdirForTest(t, tmp)

	SetSpecRawForTesting(nil)

	index, err := LoadOperationIndex()
	if err != nil {
		t.Fatalf("LoadOperationIndex: %v", err)
	}
	if index["getTests"].Path != "/tests" || string(specRaw) != string(raw) {
		t.Fatalf("unexpected loaded index: %#v", index["getTests"])
	}
}

func TestApplyOperationIDOverlay(t *testing.T) {
	t.Parallel()

	in := map[string]Operation{
		"getAlerts": {
			ID:       "getAlerts",
			Verb:     "get",
			Resource: "alerts",
			Method:   http.MethodGet,
			Path:     pathAlerts,
		},
		"getAlert": {
			ID:       "getAlert",
			Verb:     "get",
			Resource: "alert",
			Method:   http.MethodGet,
			Path:     pathAlertByID,
		},
	}

	out, err := applyOperationIDOverlay(in, &operationIDOverlay{
		Strict: true,
		Entries: map[overlayTarget]overlayOperationUpdate{
			{Path: pathAlerts, Method: http.MethodGet}: {
				OperationID: "listAlerts",
				CLICommand:  overlayAlertsListCommand,
			},
			{Path: pathAlertByID, Method: http.MethodGet}: {
				OperationID: "getAlerts",
				CLICommand:  "alerts/get",
			},
		},
	})
	if err != nil {
		t.Fatalf("applyOperationIDOverlay: %v", err)
	}

	if _, ok := out["getAlert"]; ok {
		t.Fatalf("expected getAlert key to be replaced, got %#v", out["getAlert"])
	}
	listOp := out["listAlerts"]
	if listOp.Path != pathAlerts || listOp.Resource != "alerts" || listOp.Verb != "list" {
		t.Fatalf("unexpected listAlerts operation: %#v", listOp)
	}
	if !reflect.DeepEqual(listOp.CommandPath, []string{"alerts", "list"}) {
		t.Fatalf("unexpected listAlerts command path: %#v", listOp.CommandPath)
	}
	getOp := out["getAlerts"]
	if getOp.Path != pathAlertByID || getOp.Resource != "alerts" || getOp.Verb != "get" {
		t.Fatalf("unexpected getAlerts operation: %#v", getOp)
	}
}

func TestApplyOperationIDOverlaySupportsCLICommandOnlyUpdate(t *testing.T) {
	t.Parallel()

	in := map[string]Operation{
		"getAgents": {
			ID:       "getAgents",
			Verb:     "get",
			Resource: "agents",
			Method:   http.MethodGet,
			Path:     pathAgents,
		},
	}

	out, err := applyOperationIDOverlay(in, &operationIDOverlay{
		Strict: true,
		Entries: map[overlayTarget]overlayOperationUpdate{
			{Path: pathAgents, Method: http.MethodGet}: {
				CLICommand: overlayAgentsListCommand,
			},
		},
	})
	if err != nil {
		t.Fatalf("applyOperationIDOverlay: %v", err)
	}

	op := out["getAgents"]
	if op.ID != "getAgents" {
		t.Fatalf("operationId should be unchanged when overlay omits operationId: %#v", op)
	}
	if op.Resource != "agents" || op.Verb != "list" {
		t.Fatalf("cli command override not applied: %#v", op)
	}
	if !reflect.DeepEqual(op.CommandPath, []string{"agents", "list"}) {
		t.Fatalf("cli command path not applied: %#v", op.CommandPath)
	}
}

func TestSplitCLICommand(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		input      string
		wantVerb   string
		wantRes    string
		wantPath   []string
		shouldPass bool
	}{
		{
			name:       "simple resource verb",
			input:      overlayAgentsListCommand,
			wantVerb:   "list",
			wantRes:    "agents",
			wantPath:   []string{"agents", "list"},
			shouldPass: true,
		},
		{
			name:       "underscore normalized",
			input:      "agent-settings/assign_to_cluster",
			wantVerb:   "assign-to-cluster",
			wantRes:    "agent-settings",
			wantPath:   []string{"agent-settings", "assign-to-cluster"},
			shouldPass: true,
		},
		{
			name:       "nested command path",
			input:      "quotas/account-groups/assign",
			wantVerb:   "account-groups-assign",
			wantRes:    "quotas",
			wantPath:   []string{"quotas", "account-groups", "assign"},
			shouldPass: true,
		},
		{
			name:       "reject single segment",
			input:      "listAgents",
			shouldPass: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			verb, resource, ok := SplitCLICommand(tc.input)
			if ok != tc.shouldPass {
				t.Fatalf("SplitCLICommand(%q): ok=%v want %v", tc.input, ok, tc.shouldPass)
			}
			if !tc.shouldPass {
				return
			}
			if resource != tc.wantRes || verb != tc.wantVerb {
				t.Fatalf("SplitCLICommand(%q): resource=%q verb=%q want resource=%q verb=%q", tc.input, resource, verb, tc.wantRes, tc.wantVerb)
			}
			path := CLICommandPath(tc.input)
			if !reflect.DeepEqual(path, tc.wantPath) {
				t.Fatalf("CLICommandPath(%q): got %#v want %#v", tc.input, path, tc.wantPath)
			}
		})
	}
}

func TestLoadOperationIDOverlayFromFile(t *testing.T) {
	tmp := t.TempDir()
	apiDir := filepath.Join(tmp, "api")
	if err := os.MkdirAll(apiDir, 0o755); err != nil {
		t.Fatalf(mkdirAllErrFmt, err)
	}

	specPath := filepath.Join(apiDir, testSpecFilename)
	if err := os.WriteFile(specPath, []byte(testSpecPathsYAML), 0o644); err != nil {
		t.Fatalf("WriteFile(spec): %v", err)
	}

	overlayPath := filepath.Join(apiDir, "thousandeyes.overlay.yaml")
	overlayRaw := []byte(fmt.Sprintf(`
overlay: 1.0.0
actions:
  - target: "$.paths['%s'].get"
    update:
      operationId: listAlerts
      x-thousandeyes-cli-command: %s
  - target: "$.paths['%s'].get"
    update:
      operationId: getAlerts
      x-thousandeyes-cli-command: alerts/get
`, pathAlerts, overlayAlertsListCommand, pathAlertByID))
	if err := os.WriteFile(overlayPath, overlayRaw, 0o644); err != nil {
		t.Fatalf("WriteFile(overlay): %v", err)
	}

	chdirForTest(t, tmp)

	overlay, err := loadOperationIDOverlay()
	if err != nil {
		t.Fatalf("loadOperationIDOverlay: %v", err)
	}
	if overlay == nil {
		t.Fatal("expected overlay to be loaded")
	}
	if got := overlay.Entries[overlayTarget{Path: pathAlerts, Method: http.MethodGet}]; got.OperationID != "listAlerts" || got.CLICommand != overlayAlertsListCommand {
		t.Fatalf("unexpected /alerts overlay entry: %#v", got)
	}
	if got := overlay.Entries[overlayTarget{Path: pathAlertByID, Method: http.MethodGet}]; got.OperationID != "getAlerts" || got.CLICommand != "alerts/get" {
		t.Fatalf("unexpected /alerts/{alertId} overlay entry: %#v", got)
	}
}

func TestLoadOperationIDOverlayAllowsCLICommandOnly(t *testing.T) {
	tmp := t.TempDir()
	apiDir := filepath.Join(tmp, "api")
	if err := os.MkdirAll(apiDir, 0o755); err != nil {
		t.Fatalf(mkdirAllErrFmt, err)
	}

	specPath := filepath.Join(apiDir, testSpecFilename)
	if err := os.WriteFile(specPath, []byte(testSpecPathsYAML), 0o644); err != nil {
		t.Fatalf("WriteFile(spec): %v", err)
	}

	overlayPath := filepath.Join(apiDir, "thousandeyes.overlay.yaml")
	overlayRaw := []byte(fmt.Sprintf(`
overlay: 1.0.0
actions:
  - target: "$.paths['%s'].get"
    update:
      x-thousandeyes-cli-command: %s
`, pathAgents, overlayAgentsListCommand))
	if err := os.WriteFile(overlayPath, overlayRaw, 0o644); err != nil {
		t.Fatalf("WriteFile(overlay): %v", err)
	}

	chdirForTest(t, tmp)

	overlay, err := loadOperationIDOverlay()
	if err != nil {
		t.Fatalf("loadOperationIDOverlay: %v", err)
	}
	if overlay == nil {
		t.Fatal("expected overlay to be loaded")
	}
	got := overlay.Entries[overlayTarget{Path: pathAgents, Method: http.MethodGet}]
	if got.CLICommand != overlayAgentsListCommand || got.OperationID != "" {
		t.Fatalf("unexpected overlay entry: %#v", got)
	}
}

func TestParseOperationIndexAgainstRepositorySpecSnapshot(t *testing.T) {
	specPath := repoSpecPath(t)
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", specPath, err)
	}

	index, err := ParseOperationIndex(raw)
	if err != nil {
		t.Fatalf("ParseOperationIndex(%s): %v", specPath, err)
	}
	if len(index) < 50 {
		t.Fatalf("expected many operations from repository spec, got %d", len(index))
	}

	assertRepositoryOperation(t, index, "getAlerts", http.MethodGet, pathAlerts)
	assertRepositoryOperation(t, index, "getTests", http.MethodGet, "/tests")

	createUser := index["createUser"]
	if createUser.Method != http.MethodPost || createUser.Path != "/users" {
		t.Fatalf("unexpected createUser operation: %#v", createUser)
	}
	if !createUser.HasBody {
		t.Fatalf("expected createUser to include request body metadata: %#v", createUser)
	}
	if len(createUser.RequestBody) == 0 {
		t.Fatalf("unexpected createUser request body metadata: %#v", createUser.RequestBody)
	}
	if !strings.HasSuffix(createUser.RequestBody[0].SchemaRef, "UserRequest") {
		t.Fatalf("expected createUser schemaRef to end with UserRequest, got %#v", createUser.RequestBody[0].SchemaRef)
	}
}

func assertRepositoryOperation(t *testing.T, index map[string]Operation, operationID, method, path string) {
	t.Helper()

	op, ok := index[operationID]
	if !ok {
		t.Fatalf("expected operation %q to be present", operationID)
	}
	if op.Method != method || op.Path != path {
		t.Fatalf("%s: got method=%q path=%q want method=%q path=%q", operationID, op.Method, op.Path, method, path)
	}
	if strings.TrimSpace(op.Summary) == "" {
		t.Fatalf("%s: expected summary to be populated", operationID)
	}
}

func repoSpecPath(t *testing.T) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Join(filepath.Dir(currentFile), "..", "..", "api", testSpecFilename)
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%s): %v", dir, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevWD)
	})
}

func writeTempSpecFile(t *testing.T, root string, raw []byte) string {
	t.Helper()
	specDir := filepath.Join(root, "api")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", specDir, err)
	}
	specPath := filepath.Join(specDir, testSpecFilename)
	if err := os.WriteFile(specPath, raw, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", specPath, err)
	}
	return specPath
}
