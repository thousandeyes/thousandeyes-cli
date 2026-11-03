// Copyright 2026 Cisco Systems, Inc. and its affiliates
// SPDX-License-Identifier: Apache-2.0

package synthetics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const suiteSchemaURL = "file:///thousandeyes-ci.schema.json"

type offlineSchemaLoader struct{}

func (offlineSchemaLoader) Load(location string) (any, error) {
	return nil, fmt.Errorf("external schema loading is disabled: %s", location)
}

func compileSuiteSchema(t *testing.T) (*jsonschema.Compiler, *jsonschema.Schema, map[string]any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "schemas", "thousandeyes-ci.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft7)
	compiler.UseLoader(offlineSchemaLoader{})
	if err := compiler.AddResource(suiteSchemaURL, document); err != nil {
		t.Fatal(err)
	}
	// Compilation checks the schema against the library's bundled metaschema.
	schema, err := compiler.Compile(suiteSchemaURL)
	if err != nil {
		t.Fatal(err)
	}
	if schema.DraftVersion != 7 {
		t.Fatalf("compiled draft %d, want 7", schema.DraftVersion)
	}
	return compiler, schema, document.(map[string]any)
}

func loadSchemaFixture(t *testing.T, document any) (Config, error) {
	t.Helper()
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "thousandeyes-ci.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return LoadConfig(path)
}

func TestSyntheticSchemaExamples(t *testing.T) {
	compiler, schema, document := compileSuiteSchema(t)
	data, err := os.ReadFile(filepath.Join("..", "..", "examples", "synthetics", "thousandeyes-ci.json"))
	if err != nil {
		t.Fatal(err)
	}
	example, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(example); err != nil {
		t.Fatalf("checked-in example fails schema validation: %v", err)
	}
	if _, err := loadSchemaFixture(t, example); err != nil {
		t.Fatalf("checked-in example fails CLI validation: %v", err)
	}

	examples, snippets, defaults := 0, 0, 0
	var visit func(any, string)
	visit = func(value any, pointer string) {
		switch node := value.(type) {
		case map[string]any:
			if value, ok := node["default"]; ok {
				defaults++
				fragment, err := compiler.Compile(suiteSchemaURL + "#" + pointer)
				if err != nil {
					t.Fatal(err)
				}
				if err := fragment.Validate(value); err != nil {
					t.Errorf("%s/default: %v", pointer, err)
				}
			}
			if values, ok := node["examples"].([]any); ok {
				fragment, err := compiler.Compile(suiteSchemaURL + "#" + pointer)
				if err != nil {
					t.Fatal(err)
				}
				for index, example := range values {
					examples++
					if err := fragment.Validate(example); err != nil {
						t.Errorf("%s/examples/%d: %v", pointer, index, err)
					}
					if pointer == "" {
						if _, err := loadSchemaFixture(t, example); err != nil {
							t.Errorf("root example %d fails CLI validation: %v", index, err)
						}
					}
				}
			}
			if values, ok := node["defaultSnippets"].([]any); ok {
				for index, value := range values {
					snippets++
					body := value.(map[string]any)["body"]
					suite := map[string]any{"version": 1, "tests": []any{body}}
					if err := schema.Validate(suite); err != nil {
						t.Errorf("%s/defaultSnippets/%d: %v", pointer, index, err)
					}
					if _, err := loadSchemaFixture(t, suite); err != nil {
						t.Errorf("snippet %d fails CLI validation: %v", index, err)
					}
				}
			}
			for key, child := range node {
				switch key {
				case "examples", "defaultSnippets", "default", "const", "enum":
					continue // Instance annotations are not nested schemas.
				}
				escaped := strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
				visit(child, pointer+"/"+escaped)
			}
		case []any:
			for index, child := range node {
				visit(child, fmt.Sprintf("%s/%d", pointer, index))
			}
		}
	}
	visit(document, "")
	if examples == 0 || snippets != 2 || defaults == 0 {
		t.Fatalf("expected schema examples, defaults, and both test-type snippets, got %d examples, %d defaults, and %d snippets", examples, defaults, snippets)
	}
}

func TestSyntheticSchemaConfigContract(t *testing.T) {
	_, schema, _ := compileSuiteSchema(t)
	type editFixture func(suite, test, native map[string]any)
	cases := []struct {
		name  string
		kind  string
		valid bool
		edit  editFixture
	}{
		{name: "account group flag may supply omitted ID", valid: true},
		{name: "schema metadata and defaults", valid: true, edit: func(s, test, _ map[string]any) {
			s["$schema"] = "https://schema.invalid/never-fetch.json"
			s["accountGroupId"], s["timeout"], s["pollInterval"] = nil, "", nil
			test["executionRule"], test["thresholds"] = nil, nil
		}},
		{name: "native extensions remain API owned", valid: true, edit: func(_, _, n map[string]any) {
			n["nativeExtension"] = map[string]any{"enabled": false, "zero": 0, "unset": nil, "array": []any{"x", 0, nil}}
			n["agents"] = []any{map[string]any{"agentId": "6", "agentType": nil, "nativeAgentOption": false}}
			n["port"] = "native API validates this"
			n["direction"] = "to-target"
		}},
		{name: "unknown suite field", edit: func(s, _, _ map[string]any) { s["versoin"] = 1 }},
		{name: "unknown test field", edit: func(_, test, _ map[string]any) { test["threshholds"] = map[string]any{} }},
		{name: "unknown threshold", edit: func(_, test, _ map[string]any) { test["thresholds"] = map[string]any{"maxLatnecy": 1} }},
		{name: "schema metadata must be string or null", edit: func(s, _, _ map[string]any) { s["$schema"] = true }},
		{name: "unsupported test type", edit: func(_, test, _ map[string]any) { test["type"] = "browser" }},
		{name: "unsupported direction", edit: func(_, _, n map[string]any) { n["direction"] = "from-target" }},
		{name: "missing agents", edit: func(_, _, n map[string]any) { delete(n, "agents") }},
		{name: "numeric agent JSON value", edit: func(_, _, n map[string]any) { n["agents"] = []any{map[string]any{"agentId": 6}} }},
		{name: "cluster agent", edit: func(_, _, n map[string]any) {
			n["agents"] = []any{map[string]any{"agentId": "6", "agentType": "enterprise-cluster"}}
		}},
		{name: "control in test name", edit: func(_, test, _ map[string]any) { test["name"] = "check\nname" }},
		{name: "HTTP null and zero criteria", kind: "http-server", valid: true, edit: func(_, test, _ map[string]any) {
			test["thresholds"] = map[string]any{"expectedResponseCodes": []any{}, "maxResponseTime": 0, "maxTotalTime": nil, "maxLoss": nil}
		}},
		{name: "network null and zero criteria", kind: "agent-to-server", valid: true, edit: func(_, test, _ map[string]any) {
			test["thresholds"] = map[string]any{"maxLoss": 0, "maxLatency": nil, "maxJitter": 0, "expectedResponseCodes": []any{}, "maxTotalTime": nil}
		}},
		{name: "HTTP foreign criterion", kind: "http-server", edit: func(_, test, _ map[string]any) { test["thresholds"] = map[string]any{"maxLoss": 0} }},
		{name: "network foreign criterion", kind: "agent-to-server", edit: func(_, test, _ map[string]any) {
			test["thresholds"] = map[string]any{"expectedResponseCodes": []any{200}}
		}},
		{name: "negative HTTP limit", kind: "http-server", edit: func(_, test, _ map[string]any) { test["thresholds"] = map[string]any{"maxResponseTime": -1} }},
		{name: "loss exceeds percentage", kind: "agent-to-server", edit: func(_, test, _ map[string]any) { test["thresholds"] = map[string]any{"maxLoss": 101} }},
		{name: "duplicate response codes", kind: "http-server", edit: func(_, test, _ map[string]any) {
			test["thresholds"] = map[string]any{"expectedResponseCodes": []any{200, 200}}
		}},
		{name: "protocol may be omitted", kind: "agent-to-server", valid: true, edit: func(_, _, n map[string]any) { delete(n, "protocol") }},
		{name: "null protocol is present", kind: "agent-to-server", edit: func(_, _, n map[string]any) { n["protocol"] = nil }},
		{name: "empty protocol is present", kind: "agent-to-server", edit: func(_, _, n map[string]any) { n["protocol"] = "" }},
	}
	for _, kind := range []string{"http-server", "agent-to-server"} {
		for _, item := range cases {
			if item.kind != "" && item.kind != kind {
				continue
			}
			t.Run(kind+"/"+item.name, func(t *testing.T) {
				suite, test, native := schemaContractFixture(kind)
				if item.edit != nil {
					item.edit(suite, test, native)
				}
				assertSchemaConfigValidity(t, schema, suite, item.valid)
			})
		}
		for _, field := range []string{"testId", "type", "createdDate", "modifiedDate", "savedEvent", "_links"} {
			t.Run(kind+"/read-only/"+field, func(t *testing.T) {
				suite, _, native := schemaContractFixture(kind)
				native[field] = nil // Presence is forbidden, including null.
				assertSchemaConfigValidity(t, schema, suite, false)
			})
		}
	}
}

func schemaContractFixture(kind string) (suite, test, native map[string]any) {
	native = map[string]any{"agents": []any{map[string]any{"agentId": "6"}}}
	if kind == "http-server" {
		native["url"] = "https://target.invalid/health"
	} else {
		native["server"], native["protocol"], native["port"] = "target.invalid", "tcp", 443
	}
	test = map[string]any{"name": "schema-check", "type": kind, "config": native}
	suite = map[string]any{"version": 1, "tests": []any{test}}
	return
}

func assertSchemaConfigValidity(t *testing.T, schema *jsonschema.Schema, suite map[string]any, valid bool) {
	t.Helper()
	// Use the same decoded JSON types as schema annotations and real files.
	data, err := json.Marshal(suite)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(instance); (err == nil) != valid {
		t.Errorf("schema valid=%v, want %v: %v", err == nil, valid, err)
	}
	config, err := loadSchemaFixture(t, suite)
	if (err == nil) != valid {
		t.Fatalf("CLI valid=%v, want %v: %v", err == nil, valid, err)
	}
	if metadata, ok := suite["$schema"].(string); valid && ok {
		if config.Schema != metadata {
			t.Errorf("schema metadata changed: %q", config.Schema)
		}
		encoded, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		var roundTrip map[string]any
		if err := json.Unmarshal(encoded, &roundTrip); err != nil {
			t.Fatal(err)
		}
		if roundTrip["$schema"] != metadata {
			t.Errorf("schema metadata not retained in JSON: %s", encoded)
		}
	}
}
