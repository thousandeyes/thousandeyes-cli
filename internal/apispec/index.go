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

package apispec

import (
	"bufio"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"

	specassets "github.com/thousandeyes/thousandeyes-cli/api"
	"gopkg.in/yaml.v3"
)

var (
	specOnce     sync.Once
	specRaw      []byte
	specIndex    map[string]Operation
	specIndexErr error
	specOverlay  *operationIDOverlay

	httpMethodsByName = map[string]struct{}{
		http.MethodGet:     {},
		http.MethodPost:    {},
		http.MethodPut:     {},
		http.MethodPatch:   {},
		http.MethodDelete:  {},
		http.MethodHead:    {},
		http.MethodOptions: {},
	}
)

const (
	indentLevel1       = "  "
	indentLevel2       = "    "
	yamlDescriptionKey = "description:"
	yamlRequiredKey    = "required:"
	indentLevel3       = "      "
	indentLevel4       = "        "
	indentLevel5       = "          "
	indentLevel6       = "            "
)

type operationParseState struct {
	currentPath              string
	currentMethod            string
	currentOperationID       string
	currentCLICommand        string
	currentSummary           string
	currentDescription       string
	currentDescriptionIndent string
	inParameters             bool
	inRequestBody            bool
	currentHasBody           bool
	inRequestBodyContent     bool
	currentRequestBody       []RequestBodyContent
	currentContentType       string
	inRequestBodyItems       bool
	pendingSchemaRef         bool
	currentParams            []Parameter
	inlineParam              *Parameter
	inlineDescriptionIndent  string
}

type componentParameterParseState struct {
	inComponents      bool
	inParameters      bool
	currentKey        string
	current           Parameter
	descriptionIndent string
}

func LoadOperationIndex() (map[string]Operation, error) {
	specOnce.Do(func() {
		if len(specassets.ThousandEyesSpec) == 0 {
			specIndexErr = fmt.Errorf("embedded OpenAPI spec is empty")
			return
		}

		specRaw = specassets.ThousandEyesSpec
		specIndex, specIndexErr = ParseOperationIndex(specassets.ThousandEyesSpec)
		if specIndexErr != nil {
			return
		}
		specOverlay, specIndexErr = loadOperationIDOverlay()
		if specIndexErr != nil {
			return
		}
		specIndex, specIndexErr = applyOperationIDOverlay(specIndex, specOverlay)
	})
	return specIndex, specIndexErr
}

func ParseOperationIndex(raw []byte) (map[string]Operation, error) {
	componentParams := parseComponentParameters(raw)
	index := make(map[string]Operation)
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))

	inPaths := false
	state := operationParseState{}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if skip, stop := processOperationScanLine(line, trimmed, &inPaths, componentParams, &state, func() {
			flushParsedOperation(index, &state)
		}); stop {
			break
		} else if skip {
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan OpenAPI spec: %w", err)
	}
	flushParsedOperation(index, &state)
	if len(index) == 0 {
		return nil, fmt.Errorf("no operationIds found in OpenAPI spec")
	}
	return index, nil
}

func flushParsedOperation(index map[string]Operation, state *operationParseState) {
	id := strings.TrimSpace(state.currentOperationID)
	if id == "" || state.currentPath == "" || state.currentMethod == "" {
		return
	}
	verb, resource, source, commandPath := routeFromCLICommand(strings.TrimSpace(state.currentCLICommand))
	index[id] = Operation{
		ID:            id,
		Verb:          verb,
		Resource:      resource,
		CommandPath:   append([]string(nil), commandPath...),
		CommandSource: source,
		Method:        state.currentMethod,
		Path:          state.currentPath,
		Summary:       strings.TrimSpace(state.currentSummary),
		Description:   strings.TrimSpace(state.currentDescription),
		Parameters:    append([]Parameter(nil), state.currentParams...),
		HasBody:       state.currentHasBody,
		RequestBody:   append([]RequestBodyContent(nil), state.currentRequestBody...),
	}
	state.currentOperationID = ""
}

func processOperationScanLine(line, trimmed string, inPaths *bool, componentParams map[string]Parameter, state *operationParseState, flushOperation func()) (skip bool, stop bool) {
	if consumeIndentedDescription(line, trimmed, &state.currentDescription, &state.currentDescriptionIndent) || consumeInlineParamDescription(line, trimmed, state) {
		return true, false
	}
	if !*inPaths {
		if trimmed == "paths:" {
			*inPaths = true
		}
		return true, false
	}
	if trimmed == "" {
		return true, false
	}
	if isTopLevelYAMLKey(line, trimmed) {
		return false, true
	}
	if handleOperationBoundaryLine(line, trimmed, state, flushOperation) {
		return true, false
	}
	if state.currentMethod == "" {
		return true, false
	}
	if handleOperationMetadataLine(line, trimmed, state) {
		return true, false
	}
	if state.inParameters {
		handleParameterLine(line, trimmed, componentParams, state)
		return true, false
	}
	if state.inRequestBody && handleRequestBodyLine(line, trimmed, state) {
		return true, false
	}
	return false, false
}

func handleOperationBoundaryLine(line, trimmed string, state *operationParseState, flushOperation func()) bool {
	switch {
	case isPathNode(line, trimmed):
		flushOperation()
		state.resetForPath(strings.TrimSuffix(strings.TrimSpace(line), ":"))
	case isMethodNode(line, trimmed):
		flushOperation()
		state.resetForMethod(strings.TrimSuffix(trimmed, ":"))
	default:
		return false
	}
	return true
}

func (s *operationParseState) resetOperationFields() {
	s.currentMethod = ""
	s.currentOperationID = ""
	s.currentCLICommand = ""
	s.currentSummary = ""
	s.currentDescription = ""
	s.currentDescriptionIndent = ""
	s.currentParams = nil
	s.inParameters = false
	s.inRequestBody = false
	s.currentHasBody = false
	s.inRequestBodyContent = false
	s.currentRequestBody = nil
	s.currentContentType = ""
	s.inRequestBodyItems = false
	s.pendingSchemaRef = false
	s.inlineParam = nil
	s.inlineDescriptionIndent = ""
}

func (s *operationParseState) resetForPath(path string) {
	s.currentPath = path
	s.resetOperationFields()
}

func (s *operationParseState) resetForMethod(methodName string) {
	s.resetOperationFields()
	methodName = strings.ToUpper(methodName)
	if _, ok := httpMethodsByName[methodName]; ok {
		s.currentMethod = methodName
	}
}

func isTopLevelYAMLKey(line, trimmed string) bool {
	return !strings.HasPrefix(line, " ") && strings.HasSuffix(trimmed, ":")
}

func isPathNode(line, trimmed string) bool {
	return strings.HasPrefix(line, indentLevel1+"/") && strings.HasSuffix(trimmed, ":")
}

func isMethodNode(line, trimmed string) bool {
	return strings.HasPrefix(line, indentLevel2) && !strings.HasPrefix(line, indentLevel3) && strings.HasSuffix(trimmed, ":")
}

func consumeIndentedDescription(line, trimmed string, target *string, indent *string) bool {
	if *indent == "" {
		return false
	}
	if strings.HasPrefix(line, *indent) && trimmed != "" {
		if *target == "" {
			*target = trimmed
		} else {
			*target += " " + trimmed
		}
		return true
	}
	*indent = ""
	return false
}

func consumeInlineParamDescription(line, trimmed string, state *operationParseState) bool {
	if state.inlineParam == nil {
		return false
	}
	return consumeIndentedDescription(line, trimmed, &state.inlineParam.Description, &state.inlineDescriptionIndent)
}

func handleOperationMetadataLine(line, trimmed string, state *operationParseState) bool {
	switch {
	case strings.HasPrefix(line, indentLevel3+"operationId:"):
		state.currentOperationID = strings.TrimSpace(strings.TrimPrefix(trimmed, "operationId:"))
	case strings.HasPrefix(line, indentLevel3+"x-thousandeyes-cli-command:"):
		state.currentCLICommand = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "x-thousandeyes-cli-command:")), "'\"")
	case strings.HasPrefix(line, indentLevel3+"summary:"):
		state.currentSummary = strings.TrimSpace(strings.TrimPrefix(trimmed, "summary:"))
	case strings.HasPrefix(line, indentLevel3+yamlDescriptionKey):
		setDescriptionValue(strings.TrimSpace(strings.TrimPrefix(trimmed, yamlDescriptionKey)), &state.currentDescription, &state.currentDescriptionIndent, indentLevel4)
	case strings.HasPrefix(line, indentLevel3+"parameters:"):
		state.inParameters = true
		state.inRequestBody = false
		state.inRequestBodyContent = false
		state.currentContentType = ""
		state.inRequestBodyItems = false
		state.pendingSchemaRef = false
		state.inlineParam = nil
		state.inlineDescriptionIndent = ""
	case strings.HasPrefix(line, indentLevel3+"requestBody:"):
		state.inRequestBody = true
		state.currentHasBody = true
		state.inParameters = false
		state.inRequestBodyContent = false
		state.currentContentType = ""
		state.inRequestBodyItems = false
	case strings.HasPrefix(line, indentLevel3+"responses:"), strings.HasPrefix(line, indentLevel3+"security:"):
		state.inParameters = false
		state.inRequestBody = false
		state.inRequestBodyContent = false
		state.currentContentType = ""
		state.inRequestBodyItems = false
		state.pendingSchemaRef = false
		state.inlineParam = nil
		state.inlineDescriptionIndent = ""
	default:
		return false
	}
	return true
}

func handleParameterLine(line, trimmed string, componentParams map[string]Parameter, state *operationParseState) bool {
	switch {
	case strings.HasPrefix(line, "        - $ref:"):
		refName := componentRefName(strings.TrimSpace(strings.TrimPrefix(trimmed, "- $ref:")))
		if param, ok := componentParams[refName]; ok {
			state.currentParams = append(state.currentParams, param)
		}
		state.inlineParam = nil
		state.inlineDescriptionIndent = ""
	case strings.HasPrefix(line, "        - name:"):
		state.currentParams = append(state.currentParams, Parameter{
			Name: strings.TrimSpace(strings.TrimPrefix(trimmed, "- name:")),
		})
		state.inlineParam = &state.currentParams[len(state.currentParams)-1]
		state.inlineDescriptionIndent = ""
	case state.inlineParam != nil && strings.HasPrefix(line, "          in:"):
		state.inlineParam.In = strings.TrimSpace(strings.TrimPrefix(trimmed, "in:"))
	case state.inlineParam != nil && strings.HasPrefix(line, indentLevel5+yamlRequiredKey):
		state.inlineParam.Required = strings.TrimSpace(strings.TrimPrefix(trimmed, yamlRequiredKey)) == "true"
	case state.inlineParam != nil && strings.HasPrefix(line, indentLevel5+yamlDescriptionKey):
		setDescriptionValue(strings.TrimSpace(strings.TrimPrefix(trimmed, yamlDescriptionKey)), &state.inlineParam.Description, &state.inlineDescriptionIndent, indentLevel6)
	case strings.HasPrefix(line, indentLevel4+"-"):
		state.inlineParam = nil
		state.inlineDescriptionIndent = ""
	default:
		return false
	}
	return true
}

func handleRequestBodyLine(line, trimmed string, state *operationParseState) bool {
	if consumePendingRequestBodySchemaRef(line, trimmed, state) {
		return true
	}

	switch {
	case strings.HasPrefix(line, "        content:"):
		state.inRequestBodyContent = true
		state.inRequestBodyItems = false
	case state.inRequestBodyContent && strings.HasPrefix(line, "          ") && !strings.HasPrefix(line, "            ") && strings.HasSuffix(trimmed, ":"):
		state.currentContentType = strings.TrimSuffix(trimmed, ":")
		state.currentRequestBody = append(state.currentRequestBody, RequestBodyContent{ContentType: state.currentContentType})
		state.inRequestBodyItems = false
	case state.currentContentType != "" && strings.HasPrefix(line, "            schema:"):
		state.inRequestBodyItems = false
	case state.currentContentType != "" && strings.HasPrefix(line, "              $ref:") && len(state.currentRequestBody) > 0:
		refValue := strings.TrimSpace(strings.TrimPrefix(trimmed, "$ref:"))
		if isDescriptionBlockMarker(refValue) {
			state.pendingSchemaRef = true
			return true
		}
		state.currentRequestBody[len(state.currentRequestBody)-1].SchemaRef = strings.Trim(refValue, "'\"")
	case state.currentContentType != "" && strings.HasPrefix(line, "              type:") && len(state.currentRequestBody) > 0:
		state.currentRequestBody[len(state.currentRequestBody)-1].SchemaType = strings.TrimSpace(strings.TrimPrefix(trimmed, "type:"))
	case state.currentContentType != "" && strings.HasPrefix(line, "              items:"):
		state.inRequestBodyItems = true
	case state.currentContentType != "" && state.inRequestBodyItems && strings.HasPrefix(line, "                type:") && len(state.currentRequestBody) > 0:
		state.currentRequestBody[len(state.currentRequestBody)-1].ItemsType = strings.TrimSpace(strings.TrimPrefix(trimmed, "type:"))
	default:
		return false
	}
	return true
}

func consumePendingRequestBodySchemaRef(line, trimmed string, state *operationParseState) bool {
	if !state.pendingSchemaRef {
		return false
	}
	state.pendingSchemaRef = false
	if state.currentContentType == "" || len(state.currentRequestBody) == 0 {
		return false
	}
	if !strings.HasPrefix(line, "                ") || trimmed == "" {
		return false
	}
	state.currentRequestBody[len(state.currentRequestBody)-1].SchemaRef = strings.Trim(trimmed, "'\"")
	return true
}

func setDescriptionValue(value string, target *string, indent *string, blockIndent string) {
	if value == ">-" || value == "|" || value == "|-" || value == ">" {
		*target = ""
		*indent = blockIndent
		return
	}
	*target = value
	*indent = ""
}

func parseComponentParameters(raw []byte) map[string]Parameter {
	params := map[string]Parameter{}
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	state := componentParameterParseState{}

	flush := func() {
		if state.currentKey == "" || state.current.Name == "" {
			return
		}
		params[state.currentKey] = state.current
	}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if stop := processComponentParameterLine(line, trimmed, &state, flush); stop {
			break
		}
	}

	flush()
	return params
}

func processComponentParameterLine(line, trimmed string, state *componentParameterParseState, flush func()) bool {
	if consumeIndentedDescription(line, trimmed, &state.current.Description, &state.descriptionIndent) {
		return false
	}
	if !state.inComponents {
		if trimmed == "components:" {
			state.inComponents = true
		}
		return false
	}
	if !state.inParameters {
		if strings.HasPrefix(line, indentLevel1+"parameters:") {
			state.inParameters = true
		}
		return false
	}
	if isTopLevelYAMLKey(line, trimmed) || isComponentSectionBoundaryLine(line, trimmed) {
		return true
	}
	if strings.HasPrefix(line, indentLevel2) && strings.HasSuffix(trimmed, ":") {
		flush()
		state.currentKey = strings.TrimSuffix(trimmed, ":")
		state.current = Parameter{}
		state.descriptionIndent = ""
		return false
	}
	if state.currentKey == "" {
		return false
	}
	switch {
	case strings.HasPrefix(line, indentLevel3+"name:"):
		state.current.Name = strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
	case strings.HasPrefix(line, indentLevel3+"in:"):
		state.current.In = strings.TrimSpace(strings.TrimPrefix(trimmed, "in:"))
	case strings.HasPrefix(line, indentLevel3+yamlRequiredKey):
		state.current.Required = strings.TrimSpace(strings.TrimPrefix(trimmed, yamlRequiredKey)) == "true"
	case strings.HasPrefix(line, indentLevel3+yamlDescriptionKey):
		setDescriptionValue(strings.TrimSpace(strings.TrimPrefix(trimmed, yamlDescriptionKey)), &state.current.Description, &state.descriptionIndent, indentLevel4)
	}
	return false
}

func isComponentSectionBoundaryLine(line, trimmed string) bool {
	return strings.HasPrefix(line, indentLevel1) && !strings.HasPrefix(line, indentLevel2) && strings.HasSuffix(trimmed, ":")
}

func SetSpecRawForTesting(raw []byte) {
	specOnce = sync.Once{}
	specRaw = raw
	specIndex = nil
	specIndexErr = nil
	specOverlay = nil
}

type operationIDOverlay struct {
	Strict  bool
	Entries map[overlayTarget]overlayOperationUpdate
}

type overlayTarget struct {
	Path   string
	Method string
}

type overlayOperationUpdate struct {
	OperationID string
	CLICommand  string
}

type rawOverlay struct {
	Actions []rawOverlayAction `yaml:"actions"`
}

type rawOverlayAction struct {
	Target string           `yaml:"target"`
	Update rawOverlayUpdate `yaml:"update"`
}

type rawOverlayUpdate struct {
	OperationID string `yaml:"operationId"`
	CLICommand  string `yaml:"x-thousandeyes-cli-command"`
}

var overlayTargetPattern = regexp.MustCompile(`^\$\.paths\['([^']+)'\]\.(get|post|put|patch|delete|head|options)$`)

func loadOperationIDOverlay() (*operationIDOverlay, error) {
	if len(specassets.ThousandEyesOverlay) == 0 {
		return nil, nil
	}
	return parseOperationIDOverlay(specassets.ThousandEyesOverlay, "embedded OpenAPI overlay")
}

func parseOperationIDOverlay(raw []byte, label string) (*operationIDOverlay, error) {
	var parsed rawOverlay
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("parse OpenAPI overlay %s: %w", label, err)
	}

	overlay := &operationIDOverlay{
		Strict:  true,
		Entries: make(map[overlayTarget]overlayOperationUpdate),
	}

	for idx, action := range parsed.Actions {
		target, update, include, err := parseOverlayAction(idx, action)
		if err != nil {
			return nil, err
		}
		if include {
			overlay.Entries[target] = update
		}
	}

	if len(overlay.Entries) == 0 {
		return nil, nil
	}
	return overlay, nil
}

func parseOverlayAction(index int, action rawOverlayAction) (overlayTarget, overlayOperationUpdate, bool, error) {
	targetExpr := strings.TrimSpace(action.Target)
	match := overlayTargetPattern.FindStringSubmatch(targetExpr)
	if len(match) != 3 {
		return overlayTarget{}, overlayOperationUpdate{}, false, nil
	}

	update, err := parseOverlayActionUpdate(index, targetExpr, action.Update)
	if err != nil {
		return overlayTarget{}, overlayOperationUpdate{}, false, err
	}
	target := overlayTarget{
		Path:   match[1],
		Method: strings.ToUpper(match[2]),
	}
	return target, update, true, nil
}

func parseOverlayActionUpdate(index int, targetExpr string, update rawOverlayUpdate) (overlayOperationUpdate, error) {
	actionLabel := fmt.Sprintf("overlay action #%d", index+1)
	newCLICommand := strings.TrimSpace(update.CLICommand)
	if newCLICommand == "" {
		return overlayOperationUpdate{}, fmt.Errorf("%s for target %q must set update.x-thousandeyes-cli-command", actionLabel, targetExpr)
	}
	return overlayOperationUpdate{
		OperationID: strings.TrimSpace(update.OperationID),
		CLICommand:  newCLICommand,
	}, nil
}

func applyOperationIDOverlay(index map[string]Operation, overlay *operationIDOverlay) (map[string]Operation, error) {
	if overlay == nil || len(overlay.Entries) == 0 {
		return index, nil
	}

	out := make(map[string]Operation, len(index))
	appliedTargets := make(map[overlayTarget]struct{}, len(overlay.Entries))

	for _, op := range index {
		target := overlayTarget{Path: op.Path, Method: op.Method}
		if update, ok := overlay.Entries[target]; ok {
			updatedOp, err := applyOverlayUpdateToOperation(op, target, update)
			if err != nil {
				return nil, err
			}
			op = updatedOp
			appliedTargets[target] = struct{}{}
		}
		if err := putOverlayOperation(out, op); err != nil {
			return nil, err
		}
	}

	if err := validateOverlayTargetsApplied(overlay, appliedTargets); err != nil {
		return nil, err
	}

	return out, nil
}

func applyOverlayUpdateToOperation(op Operation, target overlayTarget, update overlayOperationUpdate) (Operation, error) {
	if update.OperationID != "" {
		op.ID = update.OperationID
	}
	verb, resource, ok := SplitCLICommand(update.CLICommand)
	if !ok {
		return Operation{}, fmt.Errorf(
			"overlay target %q defines invalid x-thousandeyes-cli-command %q",
			fmt.Sprintf("%s %s", target.Method, target.Path),
			update.CLICommand,
		)
	}
	op.Verb = verb
	op.Resource = resource
	op.CommandPath = CLICommandPath(update.CLICommand)
	op.CommandSource = CommandSourceCLICommand
	return op, nil
}

func putOverlayOperation(out map[string]Operation, op Operation) error {
	if _, exists := out[op.ID]; exists {
		return fmt.Errorf("operationId collision after overlay application: %q", op.ID)
	}
	out[op.ID] = op
	return nil
}

func validateOverlayTargetsApplied(overlay *operationIDOverlay, appliedTargets map[overlayTarget]struct{}) error {
	if !overlay.Strict {
		return nil
	}
	for target := range overlay.Entries {
		if _, ok := appliedTargets[target]; ok {
			continue
		}
		return fmt.Errorf("overlay target did not match any operation: path=%q method=%q", target.Path, target.Method)
	}
	return nil
}

func routeFromCLICommand(cliCommand string) (verb, resource, source string, commandPath []string) {
	if parsedPath, ok := parseCLICommandPath(cliCommand); ok {
		parsedVerb := strings.Join(parsedPath[1:], "-")
		return parsedVerb, parsedPath[0], CommandSourceCLICommand, parsedPath
	}
	return "", "", "", nil
}

func componentRefName(ref string) string {
	ref = strings.Trim(ref, "'\"")
	const prefix = "#/components/parameters/"
	return strings.TrimPrefix(ref, prefix)
}

func SplitCLICommand(command string) (string, string, bool) {
	segments, ok := parseCLICommandPath(command)
	if !ok {
		return "", "", false
	}
	resource := segments[0]
	verb := strings.Join(segments[1:], "-")
	if resource == "" || verb == "" {
		return "", "", false
	}
	return verb, resource, true
}

func CLICommandPath(command string) []string {
	segments, ok := parseCLICommandPath(command)
	if !ok {
		return nil
	}
	return append([]string(nil), segments...)
}

func parseCLICommandPath(command string) ([]string, bool) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, false
	}

	parts := strings.Split(command, "/")
	if len(parts) < 2 {
		return nil, false
	}

	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		clean := normalizeCLICommandSegment(part)
		if clean == "" {
			return nil, false
		}
		normalized = append(normalized, clean)
	}
	return normalized, true
}

func normalizeCLICommandSegment(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, " ", "-")
	return value
}

func SplitCamelCase(value string) []string {
	if value == "" {
		return nil
	}

	var words []string
	start := 0
	for i := 1; i < len(value); i++ {
		curr := value[i]
		prev := value[i-1]

		if IsUpperASCII(curr) && (!IsUpperASCII(prev) || (i+1 < len(value) && !IsUpperASCII(value[i+1]))) {
			words = append(words, value[start:i])
			start = i
		}
	}
	words = append(words, value[start:])
	return words
}

func IsUpperASCII(b byte) bool {
	return b >= 'A' && b <= 'Z'
}
