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
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/thousandeyes/thousandeyes-cli/internal/config"
)

func testConfigProvider() config.Config {
	return config.Config{Token: "test", BaseURL: "https://api.thousandeyes.com"}
}

func TestIsJSONContentType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "problem json", value: "application/problem+json", want: true},
		{name: "plain text", value: "text/plain", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isJSONContentType(tc.value); got != tc.want {
				t.Fatalf("isJSONContentType(%q): got %v want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestAPIOperationCommandRoute(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		op           apiOperation
		wantSegments []string
		wantOK       bool
	}{
		{
			name: "uses explicit command path",
			op: apiOperation{
				ID:          "getAgents",
				Resource:    "agents",
				Verb:        "list",
				CommandPath: []string{"agents", "list"},
			},
			wantSegments: []string{"agents", "list"},
			wantOK:       true,
		},
		{
			name:         "rejects missing command path",
			op:           apiOperation{ID: "getAgents"},
			wantSegments: nil,
			wantOK:       false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			segments, ok := apiOperationCommandRoute(tc.op)
			if ok != tc.wantOK || !reflect.DeepEqual(segments, tc.wantSegments) {
				t.Fatalf("apiOperationCommandRoute(%#v): ok=%v segments=%#v want ok=%v segments=%#v", tc.op, ok, segments, tc.wantOK, tc.wantSegments)
			}
		})
	}
}

func TestAPIVerbCommandOmitsBodyFlagsWhenOperationHasNoBody(t *testing.T) {
	t.Parallel()

	resource := &cobra.Command{Use: "alerts"}
	cmd := apiVerbCommand(resource, testConfigProvider, apiOperation{
		ID:       "getAlerts",
		Resource: "alerts",
		Verb:     "list",
		Method:   http.MethodGet,
		Path:     "/alerts",
		HasBody:  false,
	}, "list")

	if flag := cmd.Flags().Lookup("body"); flag != nil {
		t.Fatalf("expected --body to be absent for GET/list operation, got: %#v", flag)
	}
	if flag := cmd.Flags().Lookup("body-file"); flag != nil {
		t.Fatalf("expected --body-file to be absent for GET/list operation, got: %#v", flag)
	}
}

func TestAPIVerbCommandDoesNotAddLegacyBodyFlagsWhenOperationHasBody(t *testing.T) {
	t.Parallel()

	resource := &cobra.Command{Use: "alerts"}
	cmd := apiVerbCommand(resource, testConfigProvider, apiOperation{
		ID:       "createAlert",
		Resource: "alerts",
		Verb:     "create",
		Method:   http.MethodPost,
		Path:     "/alerts",
		HasBody:  true,
	}, "create")

	if flag := cmd.Flags().Lookup("body"); flag != nil {
		t.Fatal("expected --body to be removed for operation with request body")
	}
	if flag := cmd.Flags().Lookup("body-file"); flag != nil {
		t.Fatal("expected --body-file to be removed for operation with request body")
	}
}

func TestRootCommandHelpGroupsStaticAndAPICommands(t *testing.T) {
	t.Parallel()

	root := &cobra.Command{Use: "thousandeyes"}
	root.PersistentFlags().String("token", "", "")
	root.AddCommand(
		&cobra.Command{Use: "version", Short: "Print the installed version"},
		&cobra.Command{
			Use:         "alerts",
			Annotations: map[string]string{rootCommandKindAnnotation: rootCommandKindAPI},
		},
		&cobra.Command{
			Use:         "users",
			Annotations: map[string]string{rootCommandKindAnnotation: rootCommandKindAPI},
		},
	)
	SetRootHelpFunc(root)

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Help(); err != nil {
		t.Fatalf("root.Help: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Usage:\n  thousandeyes [command] [flags]\n") {
		t.Fatalf("expected root usage to include command segment:\n%s", got)
	}
	if !strings.Contains(got, "API Commands:") {
		t.Fatalf("expected API commands section in help output:\n%s", got)
	}
	if !strings.Contains(got, "alerts") || !strings.Contains(got, "users") {
		t.Fatalf("expected grouped commands in help output:\n%s", got)
	}
}

func TestRegisterRootResourceCommandsRecordsErrorWhenIndexLoadFails(t *testing.T) {
	originalLoader := loadAPIOperationIndexFn
	t.Cleanup(func() {
		loadAPIOperationIndexFn = originalLoader
	})
	loadAPIOperationIndexFn = func() (map[string]apiOperation, error) {
		return nil, errors.New("missing api/thousandeyes.yaml")
	}

	root := &cobra.Command{Use: "thousandeyes"}
	RegisterRootResourceCommands(root, testConfigProvider)

	if gotErr := RootResourceRegistrationError(root); gotErr == nil {
		t.Fatal("expected registration error to be recorded")
	}
	if root.CommandPath() != "thousandeyes" {
		t.Fatalf("unexpected root command path: %s", root.CommandPath())
	}
	for _, sub := range root.Commands() {
		if sub.Name() == "api" {
			t.Fatal("did not expect raw fallback command to be registered")
		}
	}
}

func TestRootCommandHelpPrintsAPIIndexWarning(t *testing.T) {
	root := &cobra.Command{Use: "thousandeyes"}
	setRootCommandAPIIndexError(root, errors.New("could not find api/thousandeyes.yaml"))
	SetRootHelpFunc(root)

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Help(); err != nil {
		t.Fatalf("root.Help: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Warning: load API command index: could not find api/thousandeyes.yaml") {
		t.Fatalf("expected API index warning in help output, got:\n%s", got)
	}
}

func TestBuildAPIRequestBodyWithFieldOverridesUsesFieldFlags(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	bindings := []bodyFieldBinding{{JSONKey: "title", FlagName: "title", Kind: "string"}}
	registerBodyFieldFlags(cmd, bindings)

	if err := cmd.ParseFlags([]string{"--title", "from-flag"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}

	out, err := buildAPIRequestBodyWithFieldOverrides(cmd, bindings, nil)
	if err != nil {
		t.Fatalf("buildAPIRequestBodyWithFieldOverrides: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got["title"] != "from-flag" {
		t.Fatalf("title: got %#v want from-flag", got["title"])
	}
	if len(got) != 1 {
		t.Fatalf("got unexpected extra fields: %#v", got)
	}
}

func TestRegisterBodyFieldFlagsUsesPropertyDescription(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	registerBodyFieldFlags(cmd, []bodyFieldBinding{
		{
			JSONKey:     "title",
			FlagName:    "title",
			Kind:        "string",
			Description: "A human-readable title for the rule.",
		},
	})

	flag := cmd.Flags().Lookup("title")
	if flag == nil {
		t.Fatal("expected title flag to be registered")
	}
	if !strings.Contains(flag.Usage, "A human-readable title for the rule.") {
		t.Fatalf("expected usage to use schema description, got: %q", flag.Usage)
	}
}

func TestBuildBodyArrayBindingForInlineJSONStringArray(t *testing.T) {
	t.Parallel()

	resource := &cobra.Command{Use: "connectors"}
	local := &cobra.Command{Use: "update"}
	binding := buildBodyArrayBinding(resource, local, apiOperation{
		RequestBody: []apiRequestBodyContent{
			{ContentType: "application/json", SchemaType: "array", ItemsType: "string"},
		},
	})
	if binding == nil {
		t.Fatal("expected inline array binding")
	}
	if binding.FlagName != "item" {
		t.Fatalf("unexpected array binding flag name: %q", binding.FlagName)
	}
}

func TestBuildAPIRequestBodyWithArrayFlagOverrides(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	binding := &bodyArrayBinding{FlagName: "item", ItemKind: "string"}
	registerBodyArrayFlag(cmd, binding)
	if err := cmd.ParseFlags([]string{"--item", "id1", "--item", "id2"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}

	out, err := buildAPIRequestBodyWithFieldOverrides(cmd, nil, binding)
	if err != nil {
		t.Fatalf("buildAPIRequestBodyWithFieldOverrides: %v", err)
	}

	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"id1", "id2"}) {
		t.Fatalf("array body: got %#v want %#v", got, []string{"id1", "id2"})
	}
}

func TestRegisterBodyArrayFlagUsesStringTypeInHelp(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	registerBodyArrayFlag(cmd, &bodyArrayBinding{FlagName: "item", ItemKind: "string"})

	flag := cmd.Flags().Lookup("item")
	if flag == nil {
		t.Fatal("expected item flag to be registered")
	}
	if got := flag.Value.Type(); got != "string" {
		t.Fatalf("flag type: got %q want %q", got, "string")
	}
}

func TestDescribeOperationTextOrdersResourceVerb(t *testing.T) {
	t.Parallel()

	op := apiOperation{
		ID:       "getTests",
		Verb:     "get",
		Resource: "tests",
		Method:   http.MethodGet,
		Path:     "/tests",
		Summary:  "List tests",
	}
	got := describeOperationText(op)
	if !strings.Contains(got, "API Details:\n") {
		t.Fatalf("describe text should include API details section, got:\n%s", got)
	}
	if !strings.Contains(got, "  command: tests get\n") {
		t.Fatalf("describe text should include command details, got:\n%s", got)
	}
	if !strings.Contains(got, "  method:  GET\n") {
		t.Fatalf("describe text should include method details, got:\n%s", got)
	}
	if !strings.Contains(got, "  path:    /tests\n") {
		t.Fatalf("describe text should include path details, got:\n%s", got)
	}
}

func TestAPIVerbCommandHelpUsesCLIOrientedOrder(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{
		Use:   "list [flags]",
		Short: "List active alerts",
	}
	setCommandAnnotation(cmd, commandDescriptionAnnotation, "Returns a list of active alerts.")
	cmd.Flags().String("state", "", "Filter by alert state")
	cmd.Flags().String("token", "", "ThousandEyes API bearer token")
	cmd.Flags().String("base-url", "", "ThousandEyes platform base URL")
	cmd.Flags().Bool("json", false, "Print raw API payload as JSON")

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetHelpFunc(apiVerbCommandHelp)
	if err := cmd.Help(); err != nil {
		t.Fatalf("cmd.Help: %v", err)
	}

	got := out.String()
	descriptionIdx := strings.Index(got, "Returns a list of active alerts.\n")
	usageIdx := strings.Index(got, "Usage:\n")
	flagsIdx := strings.Index(got, "Flags:\n")
	sharedFlagsIdx := strings.Index(got, "Shared API flags:\n")
	if descriptionIdx == -1 || usageIdx == -1 || flagsIdx == -1 {
		t.Fatalf("expected description, usage and flags sections:\n%s", got)
	}
	if strings.Contains(got, "API Details:\n") {
		t.Fatalf("did not expect API details section in help output:\n%s", got)
	}
	if sharedFlagsIdx == -1 {
		t.Fatalf("expected shared API flags section in help output:\n%s", got)
	}
	flagsSection := got[flagsIdx:sharedFlagsIdx]
	sharedSection := got[sharedFlagsIdx:]
	if strings.Contains(flagsSection, "--help") {
		t.Fatalf("did not expect help flag in operation flags section:\n%s", got)
	}
	if !strings.Contains(sharedSection, "--help") {
		t.Fatalf("expected help flag in shared API flags section:\n%s", got)
	}
	if !strings.Contains(got, "--token string") || !strings.Contains(got, "--base-url string") {
		t.Fatalf("expected shared API flags printed one-per-line, got:\n%s", got)
	}
	if !(descriptionIdx < usageIdx && usageIdx < flagsIdx && flagsIdx < sharedFlagsIdx) {
		t.Fatalf("expected description -> usage -> flags -> shared flags order, got:\n%s", got)
	}
}

func TestAPIResourceParentHelpGroupsActionsAndSubresources(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "agents"}
	cmd.PersistentFlags().String("token", "", "ThousandEyes API bearer token")
	cmd.PersistentFlags().Bool("json", false, "Print raw API payload as JSON")

	cmd.AddCommand(
		&cobra.Command{Use: "list", Short: "List agents"},
		&cobra.Command{Use: "get", Short: "Get agent"},
	)

	cluster := &cobra.Command{Use: "cluster", Short: "Cluster operations"}
	cluster.AddCommand(&cobra.Command{Use: "assign", Short: "Assign agent cluster"})
	cmd.AddCommand(cluster)

	tests := &cobra.Command{Use: "tests", Short: "Tests operations"}
	tests.AddCommand(&cobra.Command{Use: "assign", Short: "Assign tests to agent"})
	cmd.AddCommand(tests)

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetHelpFunc(apiResourceParentHelp)
	if err := cmd.Help(); err != nil {
		t.Fatalf("cmd.Help: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Usage:\n  agents [action|sub-resource] [flags]\n") {
		t.Fatalf("expected resource usage to include action and sub-resource segment:\n%s", got)
	}
	actionsIdx := strings.Index(got, "Actions:\n")
	subResourcesIdx := strings.Index(got, "Sub-resources:\n")
	sharedFlagsIdx := strings.Index(got, "Shared API flags:\n")
	if actionsIdx == -1 || subResourcesIdx == -1 || sharedFlagsIdx == -1 {
		t.Fatalf("expected actions, sub-resources, and shared API flags sections:\n%s", got)
	}
	if !(actionsIdx < subResourcesIdx && subResourcesIdx < sharedFlagsIdx) {
		t.Fatalf("expected section order Actions -> Sub-resources -> Shared API flags:\n%s", got)
	}

	if !strings.Contains(got, "  list           List agents") || !strings.Contains(got, "  get            Get agent") {
		t.Fatalf("expected action commands in Actions section:\n%s", got)
	}
	if !strings.Contains(got, "  cluster        Cluster operations") || !strings.Contains(got, "  tests          Tests operations") {
		t.Fatalf("expected namespace commands in Sub-resources section:\n%s", got)
	}
}

func TestRegisterOperationParamFlagsUsesOpenAPIDescription(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.Flags().String("aid", "", "")
	registerOperationParamFlags(cmd, []operationParamBinding{
		{
			Name:        "aid",
			In:          "query",
			FlagName:    "aid",
			Required:    false,
			Description: "A unique identifier associated with your account group.",
		},
	})

	flag := cmd.Flags().Lookup("aid")
	if flag == nil {
		t.Fatal("expected aid flag to be registered")
	}
	if !strings.Contains(flag.Usage, "A unique identifier associated with your account group.") {
		t.Fatalf("expected usage to contain OpenAPI description, got: %q", flag.Usage)
	}
	if strings.Contains(flag.Usage, "Operation query parameter") {
		t.Fatalf("expected generic usage text to be replaced, got: %q", flag.Usage)
	}
}

func TestBuildOperationParamBindingsNormalizesFlagsToKebabCase(t *testing.T) {
	t.Parallel()

	const (
		alertIDFlag   = "alert-id"
		startDateFlag = "start-date"
		paramJSONFlag = "param-json"
	)

	apiCmd := &cobra.Command{}
	localCmd := &cobra.Command{}
	bindings := buildOperationParamBindings(localCmd, apiCmd, []apiParameter{
		{Name: "alertId", In: "path"},
		{Name: "startDate", In: "query"},
		{Name: "json", In: "query"},
	})

	if len(bindings) != 3 {
		t.Fatalf("unexpected binding count: got %d want 3", len(bindings))
	}
	if got := bindings[0].FlagName; got != alertIDFlag {
		t.Fatalf("alertId flag: got %q want %q", got, alertIDFlag)
	}
	if got := bindings[1].FlagName; got != startDateFlag {
		t.Fatalf("startDate flag: got %q want %q", got, startDateFlag)
	}
	if got := bindings[2].FlagName; got != paramJSONFlag {
		t.Fatalf("json flag: got %q want %q", got, paramJSONFlag)
	}
	if localCmd.Flags().Lookup(alertIDFlag) == nil {
		t.Fatal("expected alert-id flag to be registered")
	}
	if localCmd.Flags().Lookup(startDateFlag) == nil {
		t.Fatal("expected start-date flag to be registered")
	}
	if localCmd.Flags().Lookup(paramJSONFlag) == nil {
		t.Fatal("expected param-json flag to be registered")
	}
}

func TestResolveOperationPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		template string
		params   map[string]string
		wantPath string
		wantErr  bool
	}{
		{
			name:     "resolves and escapes path params",
			template: "/alerts/{alertId}/details/{detailId}",
			params: map[string]string{
				"alertId":  "abc/123",
				"detailId": "x y",
			},
			wantPath: "/alerts/abc%2F123/details/x%20y",
		},
		{
			name:     "rejects missing param",
			template: "/alerts/{alertId}",
			params:   map[string]string{},
			wantErr:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveOperationPath(tc.template, tc.params)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveOperationPath(%q, %#v): expected error", tc.template, tc.params)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveOperationPath(%q, %#v): %v", tc.template, tc.params, err)
			}
			if got != tc.wantPath {
				t.Fatalf("resolveOperationPath(%q, %#v): got %q want %q", tc.template, tc.params, got, tc.wantPath)
			}
		})
	}
}

func TestPrintHumanReadableObject(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := printHumanReadableJSON(&buf, map[string]any{
		"id":      "2783",
		"enabled": true,
		"details": []any{"a", "b"},
	})
	if err != nil {
		t.Fatalf("printHumanReadableJSON returned error: %v", err)
	}

	got := buf.String()
	if got != "details:\n  - a\n  - b\nenabled: true\nid: 2783\n" {
		t.Fatalf("unexpected human-readable object output: %q", got)
	}
}

func TestPrintHumanReadableArray(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := printHumanReadableJSON(&buf, []any{
		map[string]any{"id": "1"},
		map[string]any{"id": "2"},
	})
	if err != nil {
		t.Fatalf("printHumanReadableJSON returned error: %v", err)
	}

	got := buf.String()
	if got != "- id: 1\n- id: 2\n" {
		t.Fatalf("unexpected human-readable array output: %q", got)
	}
}

func TestPrintHumanReadableNestedObject(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := printHumanReadableJSON(&buf, map[string]any{
		"alerts": []any{
			map[string]any{
				"alertId": "2783",
				"state":   "ACTIVE",
			},
		},
		"_links": map[string]any{
			"next": map[string]any{
				"href": "https://api.thousandeyes.com/v7/alerts?cursor=abc123",
			},
		},
	})
	if err != nil {
		t.Fatalf("printHumanReadableJSON returned error: %v", err)
	}

	got := buf.String()
	if got != "_links:\n  next:\n    href: https://api.thousandeyes.com/v7/alerts?cursor=abc123\nalerts:\n  - alertId: 2783\n    state: ACTIVE\n" {
		t.Fatalf("unexpected human-readable array output: %q", got)
	}
}
