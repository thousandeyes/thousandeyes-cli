// Copyright 2026 Cisco Systems, Inc. and its affiliates
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestSyntheticsSchemaIsEditorMetadataOnly(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	payload := fmt.Sprintf(`{"$schema":%q,"version":1,"accountGroupId":"123","tests":[{"name":"editor-suite","type":"http-server","executionRule":"skipped","config":{"url":"https://target.invalid","agents":[{"agentId":"6"}]}}]}`, server.URL+"/unavailable-schema.json")
	stdout, stderr, err := executeSyntheticSuite(t, server.URL, payload, "--json")
	if err != nil {
		t.Fatalf("editor metadata must not affect execution: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	var report struct {
		Tests []struct {
			Status string `json:"status"`
		} `json:"tests"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Tests) != 1 || report.Tests[0].Status != "skipped" {
		t.Fatalf("unexpected skipped-suite report: %s", stdout)
	}
	if requests.Load() != 0 {
		t.Fatal("CLI must not fetch a schema or submit a skipped test")
	}
}
