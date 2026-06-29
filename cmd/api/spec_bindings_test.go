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
	"testing"

	"github.com/spf13/cobra"
	"github.com/thousandeyes/thousandeyes-cli/internal/apispec"
)

func TestBuildBodyFieldBindingsMatchesOpenAPISchema(t *testing.T) {
	index, err := loadAPIOperationIndex()
	if err != nil {
		t.Fatalf("loadAPIOperationIndex: %v", err)
	}

	apiCmd := newAPIResourceParentCommand("tests")
	checked := 0
	for _, op := range index {
		schemaRef := primaryJSONRequestSchemaRef(op)
		if schemaRef == "" {
			continue
		}

		wantProps := apispec.TopLevelPropertiesFromSchemaRef(schemaRef)
		gotBindings := buildBodyFieldBindings(apiCmd, &cobra.Command{}, op)
		if len(wantProps) == 0 {
			assertNoBindingsForEmptySchema(t, op, gotBindings)
			continue
		}
		assertMatchingBindings(t, op, wantProps, gotBindings)
		checked++
	}

	if checked == 0 {
		t.Fatal("expected at least one operation with JSON request body bindings")
	}
}

func TestBuildBodyFieldBindingsForHTTPServerInstantIncludesRequiredFlags(t *testing.T) {
	index, err := loadAPIOperationIndex()
	if err != nil {
		t.Fatalf("loadAPIOperationIndex: %v", err)
	}

	op, ok := index["createHttpServerInstantTest"]
	if !ok {
		t.Fatal("expected createHttpServerInstantTest operation")
	}

	gotBindings := buildBodyFieldBindings(newAPIResourceParentCommand("tests"), &cobra.Command{}, op)
	if len(gotBindings) == 0 {
		t.Fatal("expected request body bindings")
	}

	requiredByFlag := map[string]bool{}
	for _, binding := range gotBindings {
		requiredByFlag[binding.FlagName] = binding.Required
	}
	if !requiredByFlag["url"] {
		t.Fatal("expected --url to be marked required")
	}
	if !requiredByFlag["agents"] {
		t.Fatal("expected --agents to be marked required")
	}
}

func assertNoBindingsForEmptySchema(t *testing.T, op apiOperation, gotBindings []bodyFieldBinding) {
	t.Helper()
	if len(gotBindings) != 0 {
		t.Fatalf("%s: got %d bindings for schema with no top-level properties", op.ID, len(gotBindings))
	}
}

func assertMatchingBindings(t *testing.T, op apiOperation, wantProps []apispec.Property, gotBindings []bodyFieldBinding) {
	t.Helper()
	if len(gotBindings) != len(wantProps) {
		t.Fatalf("%s: got %d bindings want %d", op.ID, len(gotBindings), len(wantProps))
	}

	seenFlags := map[string]string{}
	for i, want := range wantProps {
		assertBindingMatches(t, op, i, want, gotBindings[i], seenFlags)
	}
}

func assertBindingMatches(t *testing.T, op apiOperation, index int, want apispec.Property, got bodyFieldBinding, seenFlags map[string]string) {
	t.Helper()
	if got.JSONKey != want.Name {
		t.Fatalf("%s binding %d: JSON key got %q want %q", op.ID, index, got.JSONKey, want.Name)
	}
	if got.Kind != want.Kind {
		t.Fatalf("%s binding %q: kind got %q want %q", op.ID, got.JSONKey, got.Kind, want.Kind)
	}
	if got.Description != want.Description {
		t.Fatalf("%s binding %q: description got %q want %q", op.ID, got.JSONKey, got.Description, want.Description)
	}
	if got.Required != want.Required {
		t.Fatalf("%s binding %q: required got %v want %v", op.ID, got.JSONKey, got.Required, want.Required)
	}
	if got.FlagName == "" {
		t.Fatalf("%s binding %q: empty flag name", op.ID, got.JSONKey)
	}
	if _, reserved := apiCmdPersistentFlagNames[got.FlagName]; reserved {
		t.Fatalf("%s binding %q: flag name %q collides with persistent api flag", op.ID, got.JSONKey, got.FlagName)
	}
	if prevKey, dup := seenFlags[got.FlagName]; dup {
		t.Fatalf("%s: duplicate flag name %q for %q and %q", op.ID, got.FlagName, prevKey, got.JSONKey)
	}
	seenFlags[got.FlagName] = got.JSONKey
}
