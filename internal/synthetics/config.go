// Copyright 2026 Cisco Systems, Inc. and its affiliates
// SPDX-License-Identifier: Apache-2.0

package synthetics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const (
	defaultTimeout      = 5 * time.Minute
	defaultPollInterval = 5 * time.Second
)

var numericID = regexp.MustCompile(`^[0-9]+$`)

type Config struct {
	Version        int          `json:"version"`
	AccountGroupID string       `json:"accountGroupId"`
	Timeout        string       `json:"timeout,omitempty"`
	PollInterval   string       `json:"pollInterval,omitempty"`
	Tests          []TestConfig `json:"tests"`
}

type TestConfig struct {
	Name          string                     `json:"name"`
	Type          string                     `json:"type"`
	ExecutionRule string                     `json:"executionRule,omitempty"`
	Config        map[string]json.RawMessage `json:"config"`
	Thresholds    Thresholds                 `json:"thresholds,omitempty"`
}

type Options struct {
	BaseURL        string
	Token          string
	AccountGroupID string
	Timeout        time.Duration
	PollInterval   time.Duration
}

type Report struct {
	Version    int          `json:"version"`
	RunID      string       `json:"runId"`
	StartedAt  time.Time    `json:"startedAt"`
	FinishedAt time.Time    `json:"finishedAt"`
	Tests      []TestResult `json:"tests"`
}

type TestResult struct {
	Name          string        `json:"name"`
	Type          string        `json:"type"`
	ExecutionRule string        `json:"executionRule"`
	TestID        string        `json:"testId,omitempty"`
	Target        string        `json:"target,omitempty"`
	Status        string        `json:"status"`
	Message       string        `json:"message,omitempty"`
	Agents        []AgentResult `json:"agents"`
}

type AgentResult struct {
	AgentID   string             `json:"agentId"`
	AgentName string             `json:"agentName,omitempty"`
	RoundID   string             `json:"roundId,omitempty"`
	Status    string             `json:"status"`
	Message   string             `json:"message,omitempty"`
	Metrics   map[string]float64 `json:"metrics,omitempty"`
	URL       string             `json:"url,omitempty"`
}

func (r Report) Failed() bool {
	for _, result := range r.Tests {
		switch result.Status {
		case "error", "timed-out":
			return true
		case "failed":
			if result.ExecutionRule != "non-blocking" {
				return true
			}
		}
	}
	return false
}

func LoadConfig(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open synthetic configuration: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	if err != nil {
		return Config{}, fmt.Errorf("read synthetic configuration: %w", err)
	}
	if len(data) > 4<<20 {
		return Config{}, fmt.Errorf("synthetic configuration exceeded size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode synthetic configuration: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Config{}, fmt.Errorf("synthetic configuration must contain one JSON object")
	}
	if err := config.validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) validate() error {
	if c.Version != 1 {
		return fmt.Errorf("synthetic configuration version must be 1")
	}
	if c.AccountGroupID != "" && !numericID.MatchString(c.AccountGroupID) {
		return fmt.Errorf("accountGroupId must be a numeric string")
	}
	if _, err := parseDuration(c.Timeout, defaultTimeout, "timeout"); err != nil {
		return err
	}
	if _, err := parseDuration(c.PollInterval, defaultPollInterval, "pollInterval"); err != nil {
		return err
	}
	if len(c.Tests) == 0 || len(c.Tests) > 100 {
		return fmt.Errorf("synthetic configuration requires between 1 and 100 tests")
	}
	names := make(map[string]bool)
	for index, test := range c.Tests {
		if err := test.validate(); err != nil {
			return fmt.Errorf("test %d: %w", index+1, err)
		}
		if names[test.Name] {
			return fmt.Errorf("test %d: duplicate test name", index+1)
		}
		names[test.Name] = true
	}
	return nil
}

func (t TestConfig) validate() error {
	if strings.TrimSpace(t.Name) == "" || len(t.Name) > 200 {
		return fmt.Errorf("name must contain between 1 and 200 bytes")
	}
	if strings.ContainsFunc(t.Name, func(character rune) bool {
		return unicode.IsControl(character) || character == '\u2028' || character == '\u2029'
	}) {
		return fmt.Errorf("name must not contain control characters or line separators")
	}
	if t.Type != "http-server" && t.Type != "agent-to-server" {
		return fmt.Errorf("unsupported synthetic test type; supported types are http-server and agent-to-server")
	}
	if t.ExecutionRule != "" && t.ExecutionRule != "blocking" && t.ExecutionRule != "non-blocking" && t.ExecutionRule != "skipped" {
		return fmt.Errorf("executionRule must be blocking, non-blocking, or skipped")
	}
	if t.Config == nil {
		return fmt.Errorf("config must be an instant-test payload object")
	}
	for _, value := range t.Config {
		if !json.Valid(value) {
			return fmt.Errorf("config contains an invalid JSON payload value")
		}
	}
	for _, field := range []string{"testId", "type", "createdDate", "modifiedDate", "savedEvent", "_links"} {
		if _, ok := t.Config[field]; ok {
			return fmt.Errorf("config must not contain read-only field %s", field)
		}
	}
	if direction, ok := t.Config["direction"]; ok && !bytes.Equal(direction, []byte(`"to-target"`)) {
		return fmt.Errorf("only to-target network direction is supported")
	}
	if t.Type == "http-server" {
		native, err := t.httpConfig()
		if err != nil {
			return err
		}
		if err := native.validate(); err != nil {
			return err
		}
	} else {
		native, err := t.networkConfig()
		if err != nil {
			return err
		}
		if err := native.validate(); err != nil {
			return err
		}
	}
	return t.Thresholds.validate(t.Type)
}

func parseDuration(value string, fallback time.Duration, field string) (time.Duration, error) {
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", field)
	}
	return duration, nil
}
