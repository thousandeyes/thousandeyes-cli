// Copyright 2026 Cisco Systems, Inc. and its affiliates
// SPDX-License-Identifier: Apache-2.0

package synthetics

import (
	"encoding/json"
	"strings"
	"testing"
)

func namedTestConfig(name string) TestConfig {
	return TestConfig{Name: name, Type: "http-server", Config: map[string]json.RawMessage{
		"url": json.RawMessage(`"https://target.invalid"`), "agents": json.RawMessage(`[{"agentId":"6"}]`),
	}}
}

func TestConfigRejectsControlCharactersInNames(t *testing.T) {
	for _, item := range []struct{ name, value string }{
		{"newline", "\n"}, {"carriage return", "\r"}, {"tab", "\t"},
		{"escape", "\x1b"}, {"null", "\x00"}, {"delete", "\x7f"},
		{"C1 control", "\u009b"}, {"next line", "\u0085"},
		{"line separator", "\u2028"}, {"paragraph separator", "\u2029"},
	} {
		t.Run(item.name, func(t *testing.T) {
			config := namedTestConfig("PRIVATE_NAME_MARKER" + item.value + "suffix")
			err := config.validate()
			if err == nil {
				t.Fatal("control character in a test name was accepted")
			}
			if strings.Contains(err.Error(), "PRIVATE_NAME_MARKER") {
				t.Fatal("validation error exposed the supplied test name")
			}
		})
	}
}

func TestConfigPreservesPrintableUnicodeNames(t *testing.T) {
	for _, name := range []string{"health check", "München 東京", "  service santé  ", "service\u2003status"} {
		config := namedTestConfig(name)
		if err := config.validate(); err != nil {
			t.Fatalf("printable name %q was rejected: %v", name, err)
		}
		if config.Name != name {
			t.Fatal("validation changed a printable test name")
		}
	}
}
