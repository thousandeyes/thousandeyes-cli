// Copyright 2026 Cisco Systems, Inc. and its affiliates
// SPDX-License-Identifier: Apache-2.0

package synthetics

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

type AgentConfig struct {
	AgentID   string `json:"agentId"`
	AgentType string `json:"agentType,omitempty"`
}

type AgentsConfig struct {
	Agents []AgentConfig `json:"agents"`
}

// These views contain only the native fields inspected by the runner. The
// complete request stays in TestConfig.Config and is not rebuilt from a view.
type HTTPConfig struct {
	AgentsConfig
	URL string `json:"url"`
}

type NetworkConfig struct {
	AgentsConfig
	Server      string `json:"server"`
	Protocol    string `json:"protocol,omitempty"`
	Port        *int   `json:"port,omitempty"`
	protocolSet bool
}

func (t TestConfig) httpConfig() (HTTPConfig, error) {
	agents, err := t.agentsConfig()
	if err != nil {
		return HTTPConfig{}, err
	}
	native := HTTPConfig{AgentsConfig: agents}
	if err := json.Unmarshal(t.Config["url"], &native.URL); err != nil {
		return HTTPConfig{}, fmt.Errorf("HTTP config requires a URL string")
	}
	return native, nil
}

func (t TestConfig) networkConfig() (NetworkConfig, error) {
	agents, err := t.agentsConfig()
	if err != nil {
		return NetworkConfig{}, err
	}
	native := NetworkConfig{AgentsConfig: agents}
	if err := json.Unmarshal(t.Config["server"], &native.Server); err != nil {
		return NetworkConfig{}, fmt.Errorf("network config requires a server string")
	}
	if raw, ok := t.Config["protocol"]; ok {
		native.protocolSet = true
		if err := json.Unmarshal(raw, &native.Protocol); err != nil {
			return NetworkConfig{}, fmt.Errorf("network protocol must be icmp, tcp, or udp")
		}
	}
	// Port is optional report context. Leave malformed values to native API
	// validation rather than strengthening the suite's existing validation here.
	var port *int
	if err := json.Unmarshal(t.Config["port"], &port); err == nil {
		native.Port = port
	}
	return native, nil
}

func (t TestConfig) agentsConfig() (AgentsConfig, error) {
	var native AgentsConfig
	if raw, ok := t.Config["agents"]; ok {
		if err := json.Unmarshal(raw, &native.Agents); err != nil {
			return AgentsConfig{}, fmt.Errorf("config requires between 1 and 100 agents")
		}
	}
	return native, nil
}

func (c HTTPConfig) validate() error {
	if _, err := c.AgentsConfig.agentIDs(); err != nil {
		return err
	}
	u, err := url.Parse(c.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("HTTP config requires an absolute http or https URL")
	}
	return nil
}

func (c NetworkConfig) validate() error {
	if _, err := c.AgentsConfig.agentIDs(); err != nil {
		return err
	}
	if strings.TrimSpace(c.Server) == "" {
		return fmt.Errorf("network config requires a server string")
	}
	if (c.protocolSet || c.Protocol != "") && c.Protocol != "icmp" && c.Protocol != "tcp" && c.Protocol != "udp" {
		return fmt.Errorf("network protocol must be icmp, tcp, or udp")
	}
	return nil
}

func (c AgentsConfig) agentIDs() ([]string, error) {
	if len(c.Agents) == 0 || len(c.Agents) > 100 {
		return nil, fmt.Errorf("config requires between 1 and 100 agents")
	}
	ids := make([]string, 0, len(c.Agents))
	seen := make(map[string]bool)
	for _, agent := range c.Agents {
		if !numericID.MatchString(agent.AgentID) || seen[agent.AgentID] {
			return nil, fmt.Errorf("config requires distinct numeric string agent IDs")
		}
		if agent.AgentType == "enterprise-cluster" {
			return nil, fmt.Errorf("cluster agent result completeness is not supported")
		}
		seen[agent.AgentID] = true
		ids = append(ids, agent.AgentID)
	}
	return ids, nil
}

func (t TestConfig) agentIDs() ([]string, error) {
	native, err := t.agentsConfig()
	if err != nil {
		return nil, err
	}
	return native.agentIDs()
}
