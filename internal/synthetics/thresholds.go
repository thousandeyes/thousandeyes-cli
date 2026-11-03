// Copyright 2026 Cisco Systems, Inc. and its affiliates
// SPDX-License-Identifier: Apache-2.0

package synthetics

import (
	"fmt"
	"math"
)

// Thresholds preserves the flat version 1 JSON object. Each adapter validates
// and evaluates only its concrete threshold type.
type Thresholds struct {
	HTTPThresholds
	NetworkThresholds
}

type HTTPThresholds struct {
	ExpectedResponseCodes []int    `json:"expectedResponseCodes,omitempty"`
	MaxResponseTime       *float64 `json:"maxResponseTime,omitempty"`
	MaxTotalTime          *float64 `json:"maxTotalTime,omitempty"`
}

type NetworkThresholds struct {
	MaxLoss    *float64 `json:"maxLoss,omitempty"`
	MaxLatency *float64 `json:"maxLatency,omitempty"`
	MaxJitter  *float64 `json:"maxJitter,omitempty"`
}

func (t Thresholds) validate(kind string) error {
	switch kind {
	case "http-server":
		if t.MaxLoss != nil || t.MaxLatency != nil || t.MaxJitter != nil {
			return fmt.Errorf("network thresholds require an agent-to-server test")
		}
		return t.HTTPThresholds.validate()
	case "agent-to-server":
		if t.MaxResponseTime != nil || t.MaxTotalTime != nil || len(t.ExpectedResponseCodes) > 0 {
			return fmt.Errorf("HTTP thresholds require an http-server test")
		}
		return t.NetworkThresholds.validate()
	default:
		return fmt.Errorf("unsupported synthetic test type")
	}
}

func (t HTTPThresholds) validate() error {
	if err := validateMaxima(map[string]*float64{"maxResponseTime": t.MaxResponseTime, "maxTotalTime": t.MaxTotalTime}); err != nil {
		return err
	}
	seenCodes := make(map[int]bool)
	for _, code := range t.ExpectedResponseCodes {
		if code < 100 || code > 599 || seenCodes[code] {
			return fmt.Errorf("expectedResponseCodes requires distinct valid HTTP status codes")
		}
		seenCodes[code] = true
	}
	return nil
}

func (t NetworkThresholds) validate() error {
	if err := validateMaxima(map[string]*float64{"maxLoss": t.MaxLoss, "maxLatency": t.MaxLatency, "maxJitter": t.MaxJitter}); err != nil {
		return err
	}
	if t.MaxLoss != nil && *t.MaxLoss > 100 {
		return fmt.Errorf("maxLoss must not exceed 100 percent")
	}
	return nil
}

func validateMaxima(values map[string]*float64) error {
	for name, value := range values {
		if value != nil && (*value < 0 || math.IsInf(*value, 0) || math.IsNaN(*value)) {
			return fmt.Errorf("%s must be a finite nonnegative number", name)
		}
	}
	return nil
}
