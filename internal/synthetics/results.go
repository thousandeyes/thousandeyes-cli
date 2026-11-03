// Copyright 2026 Cisco Systems, Inc. and its affiliates
// SPDX-License-Identifier: Apache-2.0

package synthetics

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

type link struct {
	Href string `json:"href"`
}

type resultRow struct {
	Agent struct {
		AgentID   string `json:"agentId"`
		AgentName string `json:"agentName"`
	} `json:"agent"`
	RoundID      json.Number `json:"roundId"`
	ResponseCode *float64    `json:"responseCode"`
	ResponseTime *float64    `json:"responseTime"`
	TotalTime    *float64    `json:"totalTime"`
	Loss         *float64    `json:"loss"`
	AvgLatency   *float64    `json:"avgLatency"`
	Jitter       *float64    `json:"jitter"`
	ErrorType    string      `json:"errorType"`
	ErrorDetails string      `json:"errorDetails"`
	Direction    string      `json:"direction"`
	Links        struct {
		AppLink link `json:"appLink"`
	} `json:"_links"`
}

type resultPage struct {
	Results json.RawMessage `json:"results"`
	Test    struct {
		TestID string `json:"testId"`
	} `json:"test"`
	Links struct {
		Next link `json:"next"`
	} `json:"_links"`
}

func (c *apiClient) resultRows(ctx context.Context, firstURL, testID, expectedPath string) ([]resultRow, error) {
	var rows []resultRow
	next := firstURL
	visited := make(map[string]bool)
	for next != "" {
		endpoint, err := c.resultURL(next, expectedPath)
		if err != nil {
			return nil, err
		}
		if visited[endpoint] || len(visited) >= 100 {
			return nil, fmt.Errorf("invalid or excessive result pagination")
		}
		visited[endpoint] = true
		var page resultPage
		if err := c.do(ctx, http.MethodGet, endpoint, nil, &page); err != nil {
			return nil, err
		}
		if page.Test.TestID != "" && page.Test.TestID != testID {
			return nil, fmt.Errorf("result response identifies a different test")
		}
		if len(page.Results) == 0 {
			return nil, fmt.Errorf("result response is missing its results array")
		}
		var pageRows []resultRow
		if err := json.Unmarshal(page.Results, &pageRows); err != nil {
			return nil, fmt.Errorf("invalid synthetic measurements in result response")
		}
		rows = append(rows, pageRows...)
		if len(rows) > 10000 {
			return nil, fmt.Errorf("excessive synthetic measurement count")
		}
		next = page.Links.Next.Href
	}
	return rows, nil
}

func evaluateRows(test TestConfig, ids []string, rows []resultRow) ([]AgentResult, bool, error) {
	expected := make(map[string]bool)
	for _, id := range ids {
		expected[id] = true
	}
	rounds := make(map[int64]map[string][]resultRow)
	for _, row := range rows {
		if !expected[row.Agent.AgentID] {
			return nil, false, fmt.Errorf("result response contains an unexpected or missing agent identity")
		}
		round, err := row.RoundID.Int64()
		if err != nil || round <= 0 {
			return nil, false, fmt.Errorf("result response contains an invalid round identity")
		}
		if test.Type == "agent-to-server" && row.Direction != "" && row.Direction != "to-target" {
			return nil, false, fmt.Errorf("result response uses an unsupported network direction")
		}
		if rounds[round] == nil {
			rounds[round] = make(map[string][]resultRow)
		}
		rounds[round][row.Agent.AgentID] = append(rounds[round][row.Agent.AgentID], row)
	}
	var order []int64
	for round := range rounds {
		order = append(order, round)
	}
	sort.Slice(order, func(i, j int) bool { return order[i] > order[j] })
	var latest []AgentResult
	for _, round := range order {
		outcomes := make([]AgentResult, 0, len(ids))
		complete := true
		for _, id := range ids {
			outcome := AgentResult{AgentID: id, Status: "pending", RoundID: fmt.Sprint(round)}
			agentRows := rounds[round][id]
			if len(agentRows) == 0 {
				complete = false
				outcomes = append(outcomes, outcome)
				continue
			}
			pending, failed := false, false
			for _, row := range agentRows {
				result, ready, err := evaluateRow(test, row)
				if err != nil {
					return nil, false, err
				}
				if !ready {
					pending = true
					if !failed {
						outcome = result
					}
					continue
				}
				if !failed {
					outcome = result
				}
				if result.Status == "failed" {
					failed = true
				}
			}
			if pending && !failed {
				complete = false
				outcome.Status = "pending"
				outcome.Message = "waiting for all agent measurements"
			}
			outcomes = append(outcomes, outcome)
		}
		if latest == nil {
			latest = outcomes
		}
		if complete {
			return outcomes, true, nil
		}
		// A fresh instant execution has one round. Do not use an older complete
		// round to conceal incomplete measurements from a newer round.
		break
	}
	if latest == nil {
		for _, id := range ids {
			latest = append(latest, AgentResult{AgentID: id, Status: "pending"})
		}
	}
	return latest, false, nil
}

func evaluateRow(test TestConfig, row resultRow) (AgentResult, bool, error) {
	outcome := AgentResult{AgentID: row.Agent.AgentID, AgentName: row.Agent.AgentName, RoundID: row.RoundID.String(), Status: "pending", Metrics: make(map[string]float64), URL: sanitizeAppLink(row.Links.AppLink.Href)}
	metrics := map[string]*float64{"responseCode": row.ResponseCode, "responseTime": row.ResponseTime, "totalTime": row.TotalTime}
	if test.Type == "agent-to-server" {
		metrics = map[string]*float64{"loss": row.Loss, "avgLatency": row.AvgLatency, "jitter": row.Jitter}
	}
	for key, value := range metrics {
		if value != nil {
			if math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || (key == "loss" && *value > 100) {
				return outcome, false, fmt.Errorf("result response contains an invalid numeric measurement")
			}
			outcome.Metrics[key] = *value
		}
	}
	if hasResultError(row.ErrorType) || hasResultError(row.ErrorDetails) {
		outcome.Status, outcome.Message = "failed", "agent reported a measurement error"
		if test.Type == "http-server" && row.ResponseCode != nil {
			if detail := httpResponseCodeMismatch(*row.ResponseCode, test.Thresholds.ExpectedResponseCodes); detail != "" {
				outcome.Message += "; " + detail
			}
		}
		return outcome, true, nil
	}
	switch test.Type {
	case "http-server":
		return evaluateHTTPRow(outcome, row, test.Thresholds.HTTPThresholds)
	case "agent-to-server":
		return evaluateNetworkRow(outcome, row, test.Thresholds.NetworkThresholds)
	default:
		return outcome, false, fmt.Errorf("unsupported synthetic test type")
	}
}

func evaluateHTTPRow(outcome AgentResult, row resultRow, thresholds HTTPThresholds) (AgentResult, bool, error) {
	if row.ResponseCode == nil {
		return outcome, false, nil
	}
	if *row.ResponseCode < 100 || *row.ResponseCode > 599 || math.Trunc(*row.ResponseCode) != *row.ResponseCode {
		return outcome, false, fmt.Errorf("result response contains an invalid HTTP response code")
	}
	if message := httpResponseCodeMismatch(*row.ResponseCode, thresholds.ExpectedResponseCodes); message != "" {
		outcome.Status, outcome.Message = "failed", message
		return outcome, true, nil
	}
	return evaluateLimits(outcome, []metricLimit{{"responseTime", row.ResponseTime, thresholds.MaxResponseTime}, {"totalTime", row.TotalTime, thresholds.MaxTotalTime}})
}

func httpResponseCodeMismatch(code float64, expectedCodes []int) string {
	if code < 100 || code > 599 || math.Trunc(code) != code {
		return ""
	}
	accepted := code >= 200 && code <= 399
	if len(expectedCodes) > 0 {
		accepted = false
		for _, expected := range expectedCodes {
			if float64(expected) == code {
				accepted = true
			}
		}
	}
	if accepted {
		return ""
	}
	return fmt.Sprintf("HTTP response code %g did not match expected %s", code, expectedHTTPResponseCodes(expectedCodes))
}

func expectedHTTPResponseCodes(codes []int) string {
	if len(codes) == 0 {
		return "200-399"
	}
	const maxDisplayedCodes = 10
	count := min(len(codes), maxDisplayedCodes)
	values := make([]string, count)
	for index, code := range codes[:count] {
		values[index] = strconv.Itoa(code)
	}
	expected := "[" + strings.Join(values, ", ") + "]"
	if len(codes) > count {
		expected += fmt.Sprintf(" (%d more)", len(codes)-count)
	}
	return expected
}

func evaluateNetworkRow(outcome AgentResult, row resultRow, thresholds NetworkThresholds) (AgentResult, bool, error) {
	if row.Loss == nil {
		return outcome, false, nil
	}
	maxLoss := 0.0
	if thresholds.MaxLoss != nil {
		maxLoss = *thresholds.MaxLoss
	}
	if *row.Loss > maxLoss {
		outcome.Status = "failed"
		outcome.Message = fmt.Sprintf("loss %g%% exceeded maximum %g%%", *row.Loss, maxLoss)
		return outcome, true, nil
	}
	return evaluateLimits(outcome, []metricLimit{{"avgLatency", row.AvgLatency, thresholds.MaxLatency}, {"jitter", row.Jitter, thresholds.MaxJitter}})
}

type metricLimit struct {
	name                 string
	measurement, maximum *float64
}

func evaluateLimits(outcome AgentResult, limits []metricLimit) (AgentResult, bool, error) {
	pending := false
	for _, limit := range limits {
		if limit.maximum == nil {
			continue
		}
		if limit.measurement == nil {
			pending = true
			continue
		}
		if *limit.measurement > *limit.maximum {
			outcome.Status = "failed"
			outcome.Message = fmt.Sprintf("%s %g ms exceeded maximum %g ms", limit.name, *limit.measurement, *limit.maximum)
			return outcome, true, nil
		}
	}
	if pending {
		return outcome, false, nil
	}
	outcome.Status = "passed"
	return outcome, true, nil
}

func hasResultError(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.EqualFold(value, "none")
}

func sanitizeAppLink(value string) string {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Path != "/view/tests" || u.RawPath != "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "app.thousandeyes.com" && host != "app.thousandeyes.eu" && host != "app.thousandeyes.gov" {
		return ""
	}
	query := url.Values{}
	for _, key := range []string{"__a", "testId", "roundId", "agentId"} {
		if item := u.Query().Get(key); numericID.MatchString(item) {
			query.Set(key, item)
		}
	}
	u.RawQuery = query.Encode()
	u.Fragment = ""
	return u.String()
}
