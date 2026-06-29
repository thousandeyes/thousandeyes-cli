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
	"reflect"
	"strings"
	"testing"
)

const exampleSchemaRef = "#/components/schemas/Example"

func TestSchemaHelpersExtractAndProperties(t *testing.T) {
	SetSpecRawForTesting([]byte(`
components:
  schemas:
    Base:
      type: object
      required:
        - name
      properties:
        name:
          type: string
          description: Display name
        enabled:
          type: boolean
    Child:
      type: object
      description: Child schema description
      properties:
        id:
          type: integer
    Example:
      allOf:
        - $ref: '#/components/schemas/Base'
        - type: object
          required:
            - threshold
          properties:
            threshold:
              type: number
              description: Threshold value
            child:
              $ref: '#/components/schemas/Child'
            payload:
              type: array
              items:
                type: string
            labels:
              type: array
              description: >-
                Contains list of test label IDs (get labelId from /labels
                endpoint)
              items:
                type: string
            childRef:
              $ref: '#/components/schemas/Child'
`))
	t.Cleanup(func() { SetSpecRawForTesting(nil) })

	block := LookupSchemaBlock(exampleSchemaRef)
	if !strings.Contains(block, "Example:") || !strings.Contains(block, "threshold:") {
		t.Fatalf("unexpected schema block: %q", block)
	}

	props := TopLevelPropertiesFromSchemaRef(exampleSchemaRef)
	wantProps := []Property{
		{Name: "name", Kind: "string", Description: "Display name", Required: true},
		{Name: "enabled", Kind: "bool"},
		{Name: "threshold", Kind: "number", Description: "Threshold value", Required: true},
		{Name: "child", Kind: "json", Description: "Child schema description"},
		{Name: "payload", Kind: "json"},
		{Name: "labels", Kind: "json", Description: "Contains list of test label IDs (get labelId from /labels endpoint)"},
		{Name: "childRef", Kind: "json", Description: "Child schema description"},
	}
	if !reflect.DeepEqual(props, wantProps) {
		t.Fatalf("TopLevelPropertiesFromSchemaRef: got %#v want %#v", props, wantProps)
	}

	hint := BuildSchemaPayloadHint(exampleSchemaRef)
	wantHint := "name: <string>\nenabled: false\nthreshold: 0\nchild:\n  id: 0\npayload: [<string>]\nlabels: [<string>]\nchildRef:\n  id: 0"
	if hint != wantHint {
		t.Fatalf("BuildSchemaPayloadHint: got %q want %q", hint, wantHint)
	}
}

func TestSchemaParsersAndPayloadBranches(t *testing.T) {
	SetSpecRawForTesting([]byte(`
components:
  schemas:
    Recursive:
      type: object
      properties:
        self:
          $ref: '#/components/schemas/Recursive'
    AnyOfExample:
      anyOf:
        - $ref: '#/components/schemas/HandlebarsExpression'
        - type: object
          properties:
            code:
              type: string
    ScalarArray:
      type: array
      items:
        type: integer
`))
	t.Cleanup(func() { SetSpecRawForTesting(nil) })

	if got := extractSchemaBlockWithLimit(specRaw, "Recursive", 2); !strings.Contains(got, "...") {
		t.Fatalf("expected truncated block, got %q", got)
	}
	if lookupSchemaBlockForParse("#/components/schemas/Missing") != "" {
		t.Fatal("expected missing schema parse lookup to be empty")
	}

	if got := BuildSchemaPayloadHint("#/components/schemas/AnyOfExample"); got != "code: <string>" {
		t.Fatalf("unexpected anyOf payload hint: %q", got)
	}
	if got := BuildSchemaPayloadHint("#/components/schemas/ScalarArray"); got != "- 0" {
		t.Fatalf("unexpected array payload hint: %q", got)
	}
	if got := BuildSchemaPayloadHint("#/components/schemas/Recursive"); !strings.Contains(got, "<recursive") {
		t.Fatalf("unexpected recursive payload hint: %q", got)
	}
}

func TestSchemaLowLevelHelpers(t *testing.T) {
	lines := []string{
		"  alpha:",
		"    beta: 1",
		"    gamma:",
		"      delta: true",
	}
	nodes := parseYAMLNodes(lines)
	if len(nodes) != 1 || nodes[0].Text != "alpha:" || len(nodes[0].Children) != 2 {
		t.Fatalf("unexpected YAML nodes: %#v", nodes)
	}

	if countLeadingSpaces("    value") != 4 {
		t.Fatal("countLeadingSpaces returned unexpected value")
	}
	if placeholderForSchemaType("boolean") != "false" || placeholderForSchemaType("mystery") != "<value>" {
		t.Fatal("placeholderForSchemaType returned unexpected values")
	}

	key, value, ok := splitYAMLKeyValue("type: string")
	if !ok || key != "type" || value != "string" {
		t.Fatalf("splitYAMLKeyValue: key=%q value=%q ok=%v", key, value, ok)
	}
	if _, _, ok := splitYAMLKeyValue("not-a-pair"); ok {
		t.Fatal("expected splitYAMLKeyValue to reject invalid input")
	}

	hint := &schemaHint{Type: "array", Items: &schemaHint{Type: "string"}}
	if got := scalarPayloadValue(hint, 0, map[string]bool{}); got != "[<string>]" {
		t.Fatalf("scalarPayloadValue: got %q", got)
	}
	if got := classifyBodyFieldKindDepth(&schemaHint{Ref: "#/components/schemas/DoesNotExist"}, 0); got != "json" {
		t.Fatalf("classifyBodyFieldKindDepth: got %q want json", got)
	}
	if got := collectYAMLSequenceScalars([]*yamlNode{{Text: "- name"}, {Text: "- 'threshold'"}}); !reflect.DeepEqual(got, []string{"name", "threshold"}) {
		t.Fatalf("collectYAMLSequenceScalars: got %#v", got)
	}

	parsed := parseSchemaBlock("Example:\n  type: object\n  properties:\n    name:\n      type: string\n")
	if parsed == nil || parsed.Type != "object" || parsed.Properties["name"].Type != "string" {
		t.Fatalf("unexpected parsed schema block: %#v", parsed)
	}
}
