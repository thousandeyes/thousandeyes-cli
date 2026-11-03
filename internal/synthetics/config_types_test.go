// Copyright 2026 Cisco Systems, Inc. and its affiliates
// SPDX-License-Identifier: Apache-2.0

package synthetics

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func loadTypedSuite(t *testing.T, payload string) Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "suite.json")
	if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func typedSuitePayload(kind, thresholds string) string {
	native := `{"url":"https://target.invalid","agents":[{"agentId":"6"}]}`
	if kind == "agent-to-server" {
		native = `{"server":"target.invalid","port":443,"protocol":"tcp","agents":[{"agentId":"6"}]}`
	}
	// Put the discriminator last to preserve JSON member-order independence.
	return fmt.Sprintf(`{"version":1,"accountGroupId":"123","tests":[{"name":"typed-check","config":%s,"thresholds":%s,"type":%q}]}`, native, thresholds, kind)
}

func TestTypedSuiteKeepsFlatJSONThresholdShape(t *testing.T) {
	for _, item := range []struct {
		kind, thresholds string
		keys             []string
	}{
		{"http-server", `{"expectedResponseCodes":[200],"maxResponseTime":0,"maxTotalTime":0}`, []string{"expectedResponseCodes", "maxResponseTime", "maxTotalTime"}},
		{"agent-to-server", `{"maxLoss":0,"maxLatency":0,"maxJitter":0}`, []string{"maxJitter", "maxLatency", "maxLoss"}},
	} {
		t.Run(item.kind, func(t *testing.T) {
			config := loadTypedSuite(t, typedSuitePayload(item.kind, item.thresholds))
			data, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				Tests []struct {
					Type       string                     `json:"type"`
					Config     map[string]json.RawMessage `json:"config"`
					Thresholds map[string]json.RawMessage `json:"thresholds"`
				} `json:"tests"`
			}
			if err := json.Unmarshal(data, &wire); err != nil {
				t.Fatal(err)
			}
			if len(wire.Tests) != 1 || wire.Tests[0].Type != item.kind || len(wire.Tests[0].Config) == 0 {
				t.Fatalf("suite envelope changed: %s", data)
			}
			var keys []string
			for key, value := range wire.Tests[0].Thresholds {
				keys = append(keys, key)
				if key == "expectedResponseCodes" {
					if string(value) != "[200]" {
						t.Errorf("response-code policy changed: %s", value)
					}
				} else if string(value) != "0" {
					t.Errorf("explicit zero limit %s was lost: %s", key, value)
				}
			}
			sort.Strings(keys)
			if !reflect.DeepEqual(keys, item.keys) {
				t.Fatalf("thresholds must remain flat and type-specific: %s", data)
			}
		})
	}
}

func TestTypedThresholdsPreserveOptionalAndZeroLimits(t *testing.T) {
	for _, item := range []struct {
		kind, field, metric string
		get                 func(Thresholds) *float64
	}{
		{"http-server", "maxResponseTime", "responseTime", func(value Thresholds) *float64 { return value.MaxResponseTime }},
		{"http-server", "maxTotalTime", "totalTime", func(value Thresholds) *float64 { return value.MaxTotalTime }},
		{"agent-to-server", "maxLoss", "loss", func(value Thresholds) *float64 { return value.MaxLoss }},
		{"agent-to-server", "maxLatency", "avgLatency", func(value Thresholds) *float64 { return value.MaxLatency }},
		{"agent-to-server", "maxJitter", "jitter", func(value Thresholds) *float64 { return value.MaxJitter }},
	} {
		t.Run(item.field, func(t *testing.T) {
			for _, variant := range []string{"omitted", "null", "zero"} {
				t.Run(variant, func(t *testing.T) {
					thresholds := "{}"
					if variant == "null" {
						thresholds = fmt.Sprintf(`{%q:null}`, item.field)
					} else if variant == "zero" {
						thresholds = fmt.Sprintf(`{%q:0}`, item.field)
					}
					config := loadTypedSuite(t, typedSuitePayload(item.kind, thresholds))
					test := config.Tests[0]
					limit := item.get(test.Thresholds)
					if variant == "zero" {
						if limit == nil || *limit != 0 {
							t.Fatal("zero must remain a selected limit")
						}
					} else if limit != nil {
						t.Fatal("omitted/null optional limit must remain unset")
					}
					measurements := map[string]float64{"responseCode": 200}
					if item.kind == "agent-to-server" {
						measurements = map[string]float64{"loss": 0}
					}
					check := func(want string, ready bool) {
						t.Helper()
						data, _ := json.Marshal(measurements)
						var row resultRow
						if err := json.Unmarshal(data, &row); err != nil {
							t.Fatal(err)
						}
						result, gotReady, err := evaluateRow(test, row)
						if err != nil || result.Status != want || gotReady != ready {
							t.Fatalf("measurements=%s outcome=%+v ready=%v err=%v", data, result, gotReady, err)
						}
					}
					measurements[item.metric] = 0
					check("passed", true)
					if variant == "zero" {
						measurements[item.metric] = 0.5
						check("failed", true)
						delete(measurements, item.metric)
						check("pending", false)
					} else if item.metric != "loss" {
						delete(measurements, item.metric)
						check("passed", true)
					}
				})
			}
		})
	}
}

func TestTypedThresholdsPreserveResponseCodeDefaults(t *testing.T) {
	for _, thresholds := range []string{"{}", "null", `{"expectedResponseCodes":null}`, `{"expectedResponseCodes":[]}`} {
		config := loadTypedSuite(t, typedSuitePayload("http-server", thresholds))
		for _, item := range []struct {
			code   float64
			status string
		}{{200, "passed"}, {399, "passed"}, {401, "failed"}} {
			result, ready, err := evaluateRow(config.Tests[0], resultRow{ResponseCode: &item.code})
			if err != nil || !ready || result.Status != item.status {
				t.Fatalf("thresholds=%s response=%v outcome=%+v ready=%v err=%v", thresholds, item.code, result, ready, err)
			}
		}
	}
}

func TestTypedSuiteRetainsStrictEnvelopeAndThresholdValidation(t *testing.T) {
	valid := typedSuitePayload("http-server", "{}")
	for name, payload := range map[string]string{
		"suite metadata":       strings.Replace(valid, `"version":1`, `"version":1,"futureSuiteOption":false`, 1),
		"test metadata":        strings.Replace(valid, `"name":"typed-check"`, `"name":"typed-check","futureTestOption":null`, 1),
		"threshold spelling":   typedSuitePayload("http-server", `{"maxResponseTim":null}`),
		"HTTP foreign limit":   typedSuitePayload("http-server", `{"maxLoss":0}`),
		"network foreign time": typedSuitePayload("agent-to-server", `{"maxTotalTime":0}`),
		"network HTTP policy":  typedSuitePayload("agent-to-server", `{"expectedResponseCodes":[200]}`),
		"threshold object":     typedSuitePayload("http-server", `[]`),
		"trailing document":    valid + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "suite.json")
			if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConfig(path); err == nil {
				t.Fatal("invalid suite was accepted")
			}
		})
	}
	// Foreign criteria that contain no value retain their prior unset behavior.
	loadTypedSuite(t, typedSuitePayload("http-server", `{"maxLoss":null}`))
	loadTypedSuite(t, typedSuitePayload("agent-to-server", `{"expectedResponseCodes":[]}`))
}

func TestTypedNetworkConfigPreservesProtocolPresence(t *testing.T) {
	valid := typedSuitePayload("agent-to-server", "{}")
	loadTypedSuite(t, strings.Replace(valid, `"protocol":"tcp",`, "", 1))
	for _, protocol := range []string{`null`, `""`, `false`, `"unsupported"`} {
		path := filepath.Join(t.TempDir(), "suite.json")
		payload := strings.Replace(valid, `"protocol":"tcp"`, `"protocol":`+protocol, 1)
		if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(path); err == nil {
			t.Fatalf("present invalid protocol %s was accepted", protocol)
		}
	}
}
