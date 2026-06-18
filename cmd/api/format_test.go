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
	"testing"
)

func TestPrintHumanReadableJSONQuotesAmbiguousStrings(t *testing.T) {
	payload := map[string]any{
		"alertState": "trigger",
		"details": []any{
			map[string]any{
				"metrics": `Error: "y: no such element: {"method":"xpath"}"`,
				"name":    "Tokyo, Japan",
			},
		},
	}

	var out bytes.Buffer
	if err := printHumanReadableJSON(&out, payload); err != nil {
		t.Fatalf("printHumanReadableJSON: %v", err)
	}

	got := out.String()
	wantContains := []string{
		"alertState: trigger\n",
		`metrics: "Error: \"y: no such element: {\"method\":\"xpath\"}\""` + "\n",
		`name: Tokyo, Japan` + "\n",
	}
	for _, want := range wantContains {
		if !bytes.Contains([]byte(got), []byte(want)) {
			t.Fatalf("output missing %q in:\n%s", want, got)
		}
	}
}

func TestPrintHumanReadableJSONPrintsNullLiteral(t *testing.T) {
	payload := map[string]any{
		"nextPage": nil,
	}

	var out bytes.Buffer
	if err := printHumanReadableJSON(&out, payload); err != nil {
		t.Fatalf("printHumanReadableJSON: %v", err)
	}

	if got, want := out.String(), "nextPage: null\n"; got != want {
		t.Fatalf("output mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestHumanReadableStylerKeyColorOnly(t *testing.T) {
	styler := humanReadableStyler{colorEnabled: true}

	got := styler.key("alertState")
	want := "\x1b[36malertState\x1b[0m"
	if got != want {
		t.Fatalf("unexpected styled value: got %q want %q", got, want)
	}
}

func TestFormatHumanReadableValueFloatIntegral(t *testing.T) {
	got := formatHumanReadableValue(float64(1039795))
	if got != "1039795" {
		t.Fatalf("unexpected integral float formatting: got %q", got)
	}
}
