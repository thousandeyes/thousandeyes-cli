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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/thousandeyes/thousandeyes-cli/internal/apispec"
	"github.com/thousandeyes/thousandeyes-cli/internal/cliurls"
	"github.com/thousandeyes/thousandeyes-cli/internal/config"
	"github.com/thousandeyes/thousandeyes-cli/internal/output"
	"github.com/thousandeyes/thousandeyes-cli/internal/teapi"
)

const headerContentType = "Content-Type"

func dispatchAPICall(cmd *cobra.Command, cfg config.Config, method, path string, body []byte, queryOverrides url.Values) error {
	jsonOut, _ := cmd.Flags().GetBool("json")
	query, headers := buildRequestQueryAndHeaders(queryOverrides, body)
	resp, err := executeRawAPICall(cmd, cfg, method, path, body, query, headers)
	if err != nil {
		return err
	}
	return writeAPIResponse(cmd.OutOrStdout(), resp, jsonOut)
}

func buildRequestQueryAndHeaders(queryOverrides url.Values, body []byte) (url.Values, http.Header) {
	query := url.Values{}
	applyQueryOverrides(query, queryOverrides)

	headers := http.Header{}
	if len(body) > 0 && headers.Get(headerContentType) == "" {
		headers.Set(headerContentType, "application/json")
	}
	return query, headers
}

func executeRawAPICall(cmd *cobra.Command, cfg config.Config, method, path string, body []byte, query url.Values, headers http.Header) (*teapi.RawResponse, error) {
	client := teapi.NewRawClient(cliurls.APIBaseV7(cfg.BaseURL), cfg.Token)
	return client.Do(cmd.Context(), method, path, query, headers, body)
}

func writeAPIResponse(out io.Writer, resp *teapi.RawResponse, jsonOut bool) error {
	if len(resp.Body) == 0 {
		fmt.Fprintf(out, "HTTP %d\n", resp.StatusCode)
		return nil
	}

	if isJSONContentType(resp.Header.Get("Content-Type")) || json.Valid(resp.Body) {
		var payload any
		if err := json.Unmarshal(resp.Body, &payload); err == nil {
			if jsonOut {
				return output.PrintJSON(out, payload)
			}
			return printHumanReadableJSON(out, payload)
		}
	}

	_, err := out.Write(resp.Body)
	return err
}

func runAPISingleOperation(cmd *cobra.Command, cfg config.Config, op apiOperation, paramBindings []operationParamBinding, bodyBindings []bodyFieldBinding, arrayBinding *bodyArrayBinding) error {
	pathParamOverrides, queryOverrides := parseOperationParamFlagValues(cmd, paramBindings)
	resolvedPath, err := resolveOperationPath(op.Path, pathParamOverrides)
	if err != nil {
		return fmt.Errorf("resolve operation path for %s: %w", op.ID, err)
	}

	body, err := buildAPIRequestBodyWithFieldOverrides(cmd, bodyBindings, arrayBinding)
	if err != nil {
		return err
	}

	return dispatchAPICall(cmd, cfg, op.Method, resolvedPath, body, queryOverrides)
}

func primaryJSONRequestSchemaRef(op apiOperation) string {
	body := primaryJSONRequestBody(op)
	if body == nil {
		return ""
	}
	return body.SchemaRef
}

func primaryJSONRequestBody(op apiOperation) *apiRequestBodyContent {
	for i := range op.RequestBody {
		if !isJSONContentType(op.RequestBody[i].ContentType) {
			continue
		}
		return &op.RequestBody[i]
	}
	return nil
}

func primaryJSONArrayRequestBody(op apiOperation) *apiRequestBodyContent {
	body := primaryJSONRequestBody(op)
	if body == nil || body.SchemaType != "array" {
		return nil
	}
	return body
}

func jsonPropertyKeyToFlagName(jsonKey string) string {
	parts := apispec.SplitCamelCase(jsonKey)
	for i := range parts {
		parts[i] = strings.ToLower(parts[i])
	}
	base := strings.Join(parts, "-")
	if base == "" {
		base = "field"
	}
	if _, reserved := apiCmdPersistentFlagNames[base]; reserved {
		return "json-" + base
	}
	return base
}

func allocBodyFieldFlagName(local *cobra.Command, persistRoot *cobra.Command, jsonKey string) string {
	base := jsonPropertyKeyToFlagName(jsonKey)
	return nextAvailableFlagName(local, persistRoot, base)
}

func buildBodyFieldBindings(apiCmd *cobra.Command, localCmd *cobra.Command, op apiOperation) []bodyFieldBinding {
	ref := primaryJSONRequestSchemaRef(op)
	if ref == "" {
		return nil
	}
	properties := apispec.TopLevelPropertiesFromSchemaRef(ref)
	if len(properties) == 0 {
		return nil
	}

	tmp := &cobra.Command{}
	if localCmd != nil {
		localCmd.Flags().VisitAll(func(f *pflag.Flag) {
			tmp.Flags().String(f.Name, "", "")
		})
	}
	var bindings []bodyFieldBinding
	for _, property := range properties {
		flagName := allocBodyFieldFlagName(tmp, apiCmd, property.Name)
		tmp.Flags().String(flagName, "", "")
		bindings = append(bindings, bodyFieldBinding{
			JSONKey:     property.Name,
			FlagName:    flagName,
			Kind:        property.Kind,
			Description: property.Description,
			Required:    property.Required,
		})
	}
	return bindings
}

func classifyArrayItemKind(itemType string) string {
	switch itemType {
	case "boolean":
		return "bool"
	case "integer", "number":
		return "number"
	case "string":
		return "string"
	default:
		return "json"
	}
}

func buildBodyArrayBinding(apiCmd *cobra.Command, localCmd *cobra.Command, op apiOperation) *bodyArrayBinding {
	body := primaryJSONArrayRequestBody(op)
	if body == nil {
		return nil
	}
	return &bodyArrayBinding{
		FlagName: nextAvailableFlagName(localCmd, apiCmd, "item"),
		ItemKind: classifyArrayItemKind(body.ItemsType),
	}
}

func buildOperationParamBindings(localCmd *cobra.Command, apiCmd *cobra.Command, params []apiParameter) []operationParamBinding {
	if len(params) == 0 {
		return nil
	}

	var bindings []operationParamBinding
	for _, param := range params {
		if param.In != "path" && param.In != "query" {
			continue
		}
		flagName := allocOperationParamFlagName(localCmd, apiCmd, param.Name)
		bindings = append(bindings, operationParamBinding{
			Name:        param.Name,
			In:          param.In,
			FlagName:    flagName,
			Required:    param.Required,
			Description: param.Description,
		})
		localCmd.Flags().String(flagName, "", "")
	}
	return bindings
}

func operationParamFlagBase(paramName string) string {
	name := strings.TrimSpace(paramName)
	if name == "" {
		return "param"
	}
	parts := apispec.SplitCamelCase(name)
	if len(parts) > 0 {
		for i := range parts {
			parts[i] = strings.ToLower(parts[i])
		}
		name = strings.Join(parts, "-")
	} else {
		name = strings.ToLower(name)
	}
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.Trim(name, "-")
	if name == "" {
		name = "param"
	}
	if _, reserved := apiCmdPersistentFlagNames[name]; reserved {
		return "param-" + name
	}
	return name
}

func allocOperationParamFlagName(local *cobra.Command, persistRoot *cobra.Command, paramName string) string {
	base := operationParamFlagBase(paramName)
	return nextAvailableFlagName(local, persistRoot, base)
}

func nextAvailableFlagName(local *cobra.Command, persistRoot *cobra.Command, base string) string {
	name := base
	for n := 2; ; n++ {
		if persistRoot != nil && persistRoot.PersistentFlags().Lookup(name) != nil {
			name = fmt.Sprintf("%s-%d", base, n)
			continue
		}
		if local.Flags().Lookup(name) != nil {
			name = fmt.Sprintf("%s-%d", base, n)
			continue
		}
		return name
	}
}

func registerOperationParamFlags(cmd *cobra.Command, bindings []operationParamBinding) {
	for _, binding := range bindings {
		required := "optional"
		if binding.Required {
			required = "required"
		}
		desc := formatOperationParamFlagDescription(binding, required)
		if f := cmd.Flags().Lookup(binding.FlagName); f != nil {
			f.Usage = desc
		}
	}
}

func formatOperationParamFlagDescription(binding operationParamBinding, required string) string {
	description := normalizeFlagUsageDescription(binding.Description)
	if description != "" {
		return fmt.Sprintf("%s (%s)", description, required)
	}
	return fmt.Sprintf("Operation %s parameter %q (%s)", binding.In, binding.Name, required)
}

func parseOperationParamFlagValues(cmd *cobra.Command, bindings []operationParamBinding) (map[string]string, url.Values) {
	pathParams := map[string]string{}
	query := url.Values{}
	for _, binding := range bindings {
		flag := cmd.Flags().Lookup(binding.FlagName)
		if flag == nil || !flag.Changed {
			continue
		}
		value := strings.TrimSpace(flag.Value.String())
		switch binding.In {
		case "path":
			pathParams[binding.Name] = value
		case "query":
			query.Set(binding.Name, value)
		}
	}
	return pathParams, query
}

func applyQueryOverrides(query url.Values, overrides url.Values) {
	for key, values := range overrides {
		delete(query, key)
		for _, value := range values {
			query.Add(key, value)
		}
	}
}

func registerBodyFieldFlags(cmd *cobra.Command, bindings []bodyFieldBinding) {
	for _, b := range bindings {
		desc := formatBodyFieldFlagDescription(b)
		cmd.Flags().String(b.FlagName, "", desc)
	}
}

func registerBodyArrayFlag(cmd *cobra.Command, binding *bodyArrayBinding) {
	if binding == nil {
		return
	}
	desc := formatBodyArrayFlagDescription(binding)
	cmd.Flags().Var(newRepeatableArrayFlagValue(arrayFlagTypeName(binding.ItemKind)), binding.FlagName, desc)
}

func formatBodyFieldFlagDescription(binding bodyFieldBinding) string {
	description := normalizeFlagUsageDescription(binding.Description)
	if description != "" {
		if binding.Required {
			return fmt.Sprintf("%s (required)", description)
		}
		return description
	}
	if binding.Required {
		return fmt.Sprintf("JSON request body field %q (required)", binding.JSONKey)
	}
	return fmt.Sprintf("JSON request body field %q", binding.JSONKey)
}

func formatBodyArrayFlagDescription(binding *bodyArrayBinding) string {
	if binding == nil {
		return ""
	}
	if description := normalizeFlagUsageDescription(binding.Description); description != "" {
		return description
	}
	return "JSON request body array item (repeat flag for multiple items)"
}

func normalizeFlagUsageDescription(description string) string {
	description = strings.Join(strings.Fields(strings.TrimSpace(description)), " ")
	// pflag treats backtick-delimited words in usage text as metavariables (e.g. `id`),
	// which rewrites the rendered type column. Strip backticks to preserve normal flag output.
	return strings.ReplaceAll(description, "`", "")
}

func parseBodyFlagValue(s string, kind string) (any, error) {
	switch kind {
	case "bool":
		v, err := strconv.ParseBool(s)
		if err != nil {
			return nil, fmt.Errorf("expected true or false: %w", err)
		}
		return v, nil
	case "number":
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return float64(i), nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("expected number: %w", err)
		}
		return f, nil
	case "string":
		return s, nil
	default:
		var v any
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			return nil, fmt.Errorf("expected JSON value: %w", err)
		}
		return v, nil
	}
}

func buildAPIRequestBodyWithFieldOverrides(cmd *cobra.Command, bindings []bodyFieldBinding, arrayBinding *bodyArrayBinding) ([]byte, error) {
	if arrayBinding != nil {
		return buildAPIRequestBodyFromArrayFlag(cmd, arrayBinding)
	}

	if len(bindings) == 0 {
		return nil, nil
	}

	if missing := missingRequiredBodyFlags(cmd, bindings); len(missing) > 0 {
		return nil, fmt.Errorf("missing required request body flags: %s", strings.Join(missing, ", "))
	}

	if !hasChangedBodyFieldFlag(cmd, bindings) {
		return nil, nil
	}

	base := map[string]any{}

	if err := applyBodyFieldOverrides(cmd, bindings, base); err != nil {
		return nil, err
	}

	out, err := json.Marshal(base)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func buildAPIRequestBodyFromArrayFlag(cmd *cobra.Command, binding *bodyArrayBinding) ([]byte, error) {
	if binding == nil {
		return nil, nil
	}
	f := cmd.Flags().Lookup(binding.FlagName)
	if f == nil || !f.Changed {
		return nil, nil
	}

	items, err := readArrayFlagValues(f)
	if err != nil {
		return nil, fmt.Errorf("read --%s values: %w", binding.FlagName, err)
	}
	payload := make([]any, 0, len(items))
	for _, item := range items {
		raw := item
		if binding.ItemKind != "string" {
			raw = strings.TrimSpace(item)
		}
		val, err := parseBodyFlagValue(raw, binding.ItemKind)
		if err != nil {
			return nil, fmt.Errorf("--%s: %w", binding.FlagName, err)
		}
		payload = append(payload, val)
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func arrayFlagTypeName(itemKind string) string {
	switch itemKind {
	case "bool", "number", "json":
		return itemKind
	default:
		return "string"
	}
}

type repeatableArrayFlagValue struct {
	items    []string
	typeName string
}

func newRepeatableArrayFlagValue(typeName string) *repeatableArrayFlagValue {
	return &repeatableArrayFlagValue{
		typeName: typeName,
	}
}

func (v *repeatableArrayFlagValue) String() string {
	return strings.Join(v.items, ",")
}

func (v *repeatableArrayFlagValue) Set(value string) error {
	v.items = append(v.items, value)
	return nil
}

func (v *repeatableArrayFlagValue) Type() string {
	return v.typeName
}

func readArrayFlagValues(flag *pflag.Flag) ([]string, error) {
	if flag == nil {
		return nil, nil
	}
	value, ok := flag.Value.(*repeatableArrayFlagValue)
	if !ok {
		return nil, fmt.Errorf("unexpected array flag type %T", flag.Value)
	}
	return append([]string(nil), value.items...), nil
}

func hasChangedBodyFieldFlag(cmd *cobra.Command, bindings []bodyFieldBinding) bool {
	for _, b := range bindings {
		if f := cmd.Flags().Lookup(b.FlagName); f != nil && f.Changed {
			return true
		}
	}
	return false
}

func missingRequiredBodyFlags(cmd *cobra.Command, bindings []bodyFieldBinding) []string {
	var missing []string
	for _, binding := range bindings {
		if !binding.Required {
			continue
		}
		flag := cmd.Flags().Lookup(binding.FlagName)
		if flag == nil || !flag.Changed || strings.TrimSpace(flag.Value.String()) == "" {
			missing = append(missing, "--"+binding.FlagName)
		}
	}
	return missing
}

func applyBodyFieldOverrides(cmd *cobra.Command, bindings []bodyFieldBinding, base map[string]any) error {
	for _, b := range bindings {
		f := cmd.Flags().Lookup(b.FlagName)
		if f == nil || !f.Changed {
			continue
		}
		val, err := parseBodyFlagValue(strings.TrimSpace(f.Value.String()), b.Kind)
		if err != nil {
			return fmt.Errorf("--%s: %w", b.FlagName, err)
		}
		base[b.JSONKey] = val
	}
	return nil
}
