// Copyright 2026 Cisco Systems, Inc. and its affiliates
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func syntheticNamedSuite(t *testing.T, name, rule string) string {
	t.Helper()
	var suite map[string]any
	if err := json.Unmarshal([]byte(singleHTTPConfig(rule)), &suite); err != nil {
		t.Fatal(err)
	}
	suite["tests"].([]any)[0].(map[string]any)["name"] = name
	payload, err := json.Marshal(suite)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func TestSyntheticsRejectsUnsafeNamesBeforeExecution(t *testing.T) {
	server, requests := syntheticPathServer(t)
	for _, item := range []struct{ name, value, rule string }{
		{"workflow command", "PRIVATE_NAME_MARKER\n::error:: forged annotation", "blocking"},
		{"terminal escape", "PRIVATE_NAME_MARKER\x1b[2J\x1b[H", "blocking"},
		{"skipped test", "PRIVATE_NAME_MARKER\n::warning:: forged annotation\x1b[2J", "skipped"},
		{"line separator", "PRIVATE_NAME_MARKER\u2028forged line", "blocking"},
		{"paragraph separator", "PRIVATE_NAME_MARKER\u2029forged paragraph", "blocking"},
	} {
		t.Run(item.name, func(t *testing.T) {
			directory := t.TempDir()
			jsonPath, junitPath := filepath.Join(directory, "result.json"), filepath.Join(directory, "result.xml")
			before := requests.Load()
			stdout, stderr, err := executeSyntheticSuite(t, server.URL, syntheticNamedSuite(t, item.value, item.rule),
				"--json-report", jsonPath, "--junit-report", junitPath)
			if err == nil {
				t.Error("unsafe name was accepted")
			}
			if requests.Load() != before {
				t.Error("invalid test name submitted an API request")
			}
			output := stdout + stderr
			if err != nil {
				output += err.Error()
			}
			for _, value := range []string{"PRIVATE_NAME_MARKER", "::error::", "::warning::", "\x1b", "\u2028", "\u2029"} {
				if strings.Contains(output, value) {
					t.Errorf("unsafe name content %q was echoed", value)
				}
			}
			for _, path := range []string{jsonPath, junitPath} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Errorf("report created for invalid suite: %v", err)
				}
			}
		})
	}
}

func TestSyntheticsPreservesPrintableUnicodeNames(t *testing.T) {
	server, _ := syntheticPathServer(t)
	const name = "München health 東京"
	path := filepath.Join(t.TempDir(), "result.json")
	stdout, stderr, err := executeSyntheticSuite(t, server.URL, syntheticNamedSuite(t, name, "blocking"), "--json-report", path)
	if err != nil {
		t.Fatalf("valid Unicode name failed: %v\n%s\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "PASSED "+name) {
		t.Fatal("terminal report changed the printable test name")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Tests []struct{ Name string } `json:"tests"`
	}
	if err := json.Unmarshal(data, &report); err != nil || len(report.Tests) != 1 || report.Tests[0].Name != name {
		t.Fatalf("JSON report changed the printable test name: %v", err)
	}
}
