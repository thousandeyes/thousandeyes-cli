// Copyright 2026 Cisco Systems, Inc. and its affiliates
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func syntheticPathServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method == http.MethodPost {
			syntheticJSON(t, w, http.StatusCreated, map[string]any{"testId": "101", "agents": []any{
				map[string]any{"agentId": "6", "agentType": "cloud"},
			}})
			return
		}
		syntheticJSON(t, w, http.StatusOK, map[string]any{"results": []any{
			map[string]any{"agent": map[string]any{"agentId": "6"}, "roundId": 1793268000, "responseCode": 200},
		}})
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func executeSyntheticPaths(t *testing.T, baseURL, configPath string, extra ...string) error {
	t.Helper()
	t.Setenv("TE_TOKEN", "synthetics-path-test-token")
	t.Setenv("TE_BASE_URL", baseURL)
	command := newTestRootCommand(t)
	captureCommandIO(t, command)
	command.SetArgs(append([]string{"synthetics", "run-tests", "--config", configPath}, extra...))
	return command.Execute()
}

func symlinkSyntheticPath(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symbolic links are unavailable: %v", err)
		}
		t.Fatal(err)
	}
}

func TestSyntheticsReportPathsRejectAliasedFilesBeforeExecution(t *testing.T) {
	for _, scenario := range []string{"configuration-parent-alias", "configuration-parent-dotdot", "configuration-file-alias", "configuration-case-alias", "report-parent-alias", "report-case-alias"} {
		t.Run(scenario, func(t *testing.T) {
			server, requests := syntheticPathServer(t)
			directory := t.TempDir()
			alias := filepath.Join(t.TempDir(), "alias")
			symlinkSyntheticPath(t, directory, alias)
			suite := filepath.Join(directory, "suite.json")
			payload := singleHTTPConfig("blocking")
			if err := os.WriteFile(suite, []byte(payload), 0600); err != nil {
				t.Fatal(err)
			}
			configPath := suite
			var flags []string
			switch scenario {
			case "configuration-parent-alias":
				flags = []string{"--json-report", filepath.Join(alias, "suite.json")}
			case "configuration-parent-dotdot":
				nested := filepath.Join(directory, "nested")
				if err := os.Mkdir(nested, 0700); err != nil {
					t.Fatal(err)
				}
				nestedAlias := filepath.Join(t.TempDir(), "nested-alias")
				symlinkSyntheticPath(t, nested, nestedAlias)
				flags = []string{"--json-report", nestedAlias + string(filepath.Separator) + ".." + string(filepath.Separator) + "suite.json"}
			case "configuration-file-alias":
				configPath = filepath.Join(directory, "config-link.json")
				symlinkSyntheticPath(t, suite, configPath)
				flags = []string{"--json-report", suite}
			case "configuration-case-alias":
				flags = []string{"--json-report", filepath.Join(directory, "SUITE.JSON")}
			case "report-parent-alias":
				flags = []string{"--json-report", filepath.Join(directory, "result.json"), "--junit-report", filepath.Join(alias, "result.json")}
			case "report-case-alias":
				flags = []string{"--json-report", filepath.Join(directory, "result.json"), "--junit-report", filepath.Join(alias, "RESULT.JSON")}
			}
			err := executeSyntheticPaths(t, server.URL, configPath, flags...)
			if err == nil || !strings.Contains(err.Error(), "different paths") {
				t.Errorf("expected report path collision rejection, got %v", err)
			}
			if requests.Load() != 0 {
				t.Errorf("path collision submitted %d API requests", requests.Load())
			}
			data, readErr := os.ReadFile(suite)
			if readErr != nil || string(data) != payload {
				t.Errorf("configuration was changed: read error=%v", readErr)
			}
			if _, statErr := os.Stat(filepath.Join(directory, "result.json")); !os.IsNotExist(statErr) {
				t.Errorf("colliding report was created: %v", statErr)
			}
		})
	}
}

func TestSyntheticsReportPathsAllowDistinctAliasedOutputs(t *testing.T) {
	for _, scenario := range []string{"parent-alias", "final-output-symlink", "distinct-case-variant-parents"} {
		t.Run(scenario, func(t *testing.T) {
			server, requests := syntheticPathServer(t)
			directory := t.TempDir()
			alias := filepath.Join(t.TempDir(), "alias")
			symlinkSyntheticPath(t, directory, alias)
			suite := filepath.Join(directory, "suite.json")
			payload := singleHTTPConfig("blocking")
			if err := os.WriteFile(suite, []byte(payload), 0600); err != nil {
				t.Fatal(err)
			}
			jsonPath := filepath.Join(alias, "result.json")
			junitPath := filepath.Join(directory, "result.xml")
			if scenario == "final-output-symlink" {
				// Atomic rename must replace this entry rather than follow it to
				// the configuration file that was read by the command.
				symlinkSyntheticPath(t, suite, jsonPath)
			}
			if scenario == "distinct-case-variant-parents" {
				upper, lower := filepath.Join(directory, "Reports"), filepath.Join(directory, "reports")
				if err := os.Mkdir(upper, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(lower, 0700); err != nil {
					if os.IsExist(err) {
						t.Skip("filesystem does not support distinct case-variant directories")
					}
					t.Fatal(err)
				}
				jsonPath, junitPath = filepath.Join(upper, "result.json"), filepath.Join(lower, "RESULT.JSON")
			}
			if err := executeSyntheticPaths(t, server.URL, suite, "--json-report", jsonPath, "--junit-report", junitPath); err != nil {
				t.Fatalf("distinct output entries should be accepted: %v", err)
			}
			if requests.Load() != 2 {
				t.Errorf("expected one creation and one result request, got %d", requests.Load())
			}
			data, err := os.ReadFile(suite)
			if err != nil || string(data) != payload {
				t.Fatalf("configuration was changed: read error=%v", err)
			}
			jsonData, err := os.ReadFile(jsonPath)
			if err != nil || !json.Valid(jsonData) {
				t.Fatalf("JSON output missing or invalid: %v", err)
			}
			xmlData, err := os.ReadFile(junitPath)
			if err != nil {
				t.Fatal(err)
			}
			var junit struct {
				XMLName xml.Name `xml:"testsuite"`
				Tests   int      `xml:"tests,attr"`
			}
			if err := xml.Unmarshal(xmlData, &junit); err != nil || junit.Tests != 1 {
				t.Fatalf("JUnit output missing or invalid: %v", err)
			}
			info, err := os.Lstat(jsonPath)
			if err != nil || !info.Mode().IsRegular() {
				t.Errorf("atomic report output must be a regular file: %v", err)
			}
		})
	}
}
