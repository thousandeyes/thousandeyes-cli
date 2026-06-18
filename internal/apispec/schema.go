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
	"strings"
)

func LookupSchemaBlock(schemaRef string) string {
	if specRaw == nil || schemaRef == "" {
		return ""
	}
	const prefix = "#/components/schemas/"
	name := strings.TrimPrefix(schemaRef, prefix)
	if name == schemaRef || name == "" {
		return ""
	}
	return extractSchemaBlock(specRaw, name)
}

func BuildSchemaPayloadHint(schemaRef string) string {
	schema := parseSchemaRef(schemaRef)
	if schema == nil {
		return ""
	}

	lines := buildPayloadLines(schema, 0, map[string]bool{})
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func TopLevelPropertiesFromSchemaRef(schemaRef string) []Property {
	root := parseSchemaRef(schemaRef)
	if root == nil {
		return nil
	}
	props := make(map[string]*schemaHint)
	var order []string
	mergeTopLevelSchemaProperties(root, map[string]bool{}, props, &order)
	if len(order) == 0 {
		return nil
	}

	properties := make([]Property, 0, len(order))
	for _, name := range order {
		properties = append(properties, Property{
			Name:        name,
			Kind:        classifyBodyFieldKind(props[name]),
			Description: resolveSchemaDescription(props[name], map[string]bool{}, 0),
		})
	}
	return properties
}

func resolveSchemaDescription(h *schemaHint, seen map[string]bool, depth int) string {
	if h == nil || depth > 16 {
		return ""
	}
	if description := strings.TrimSpace(h.Description); description != "" {
		return description
	}
	if h.Ref != "" {
		if seen[h.Ref] {
			return ""
		}
		seen[h.Ref] = true
		defer delete(seen, h.Ref)
		if resolved := parseSchemaRef(h.Ref); resolved != nil {
			return resolveSchemaDescription(resolved, seen, depth+1)
		}
	}
	for _, item := range h.AllOf {
		if description := resolveSchemaDescription(item, seen, depth+1); description != "" {
			return description
		}
	}
	for _, item := range h.AnyOf {
		if description := resolveSchemaDescription(item, seen, depth+1); description != "" {
			return description
		}
	}
	return ""
}

func mergeTopLevelSchemaProperties(sch *schemaHint, seen map[string]bool, props map[string]*schemaHint, order *[]string) {
	if sch == nil {
		return
	}
	if sch.Ref != "" {
		if seen[sch.Ref] {
			return
		}
		seen[sch.Ref] = true
		defer delete(seen, sch.Ref)
		if resolved := parseSchemaRef(sch.Ref); resolved != nil {
			mergeTopLevelSchemaProperties(resolved, seen, props, order)
		}
		return
	}
	for _, item := range sch.AllOf {
		mergeTopLevelSchemaProperties(item, seen, props, order)
	}
	for _, name := range sch.PropertyOrder {
		if _, exists := props[name]; !exists {
			*order = append(*order, name)
		}
		props[name] = sch.Properties[name]
	}
}

func classifyBodyFieldKind(h *schemaHint) string {
	return classifyBodyFieldKindDepth(h, 0)
}

func classifyBodyFieldKindDepth(h *schemaHint, depth int) string {
	if h == nil || depth > 16 {
		return "json"
	}
	if h.Ref != "" {
		if resolved := parseSchemaRef(h.Ref); resolved != nil {
			return classifyBodyFieldKindDepth(resolved, depth+1)
		}
		return "json"
	}
	switch h.Type {
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

func extractSchemaBlock(raw []byte, schemaName string) string {
	return extractSchemaBlockWithLimit(raw, schemaName, 24)
}

func extractSchemaBlockFull(raw []byte, schemaName string) string {
	return extractSchemaBlockWithLimit(raw, schemaName, 0)
}

func extractSchemaBlockWithLimit(raw []byte, schemaName string, maxLines int) string {
	lines := collectSchemaBlockLines(raw, schemaName)
	if len(lines) == 0 {
		return ""
	}
	lines = trimSchemaBlockLines(lines, maxLines)
	return strings.Join(lines, "\n")
}

func collectSchemaBlockLines(raw []byte, schemaName string) []string {
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	inSchemas := false
	collecting := false
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if processSchemaBlockLine(line, trimmed, schemaName, &inSchemas, &collecting, &lines) {
			break
		}
	}
	return lines
}

func trimSchemaBlockLines(lines []string, maxLines int) []string {
	if maxLines > 0 && len(lines) > maxLines {
		return append(lines[:maxLines], "...")
	}
	return lines
}

func processSchemaBlockLine(line, trimmed, schemaName string, inSchemas, collecting *bool, lines *[]string) bool {
	if !*inSchemas && isSchemasSectionStart(line, trimmed) {
		*inSchemas = true
		return false
	}
	if !*inSchemas {
		return false
	}
	if isComponentSectionBoundary(line, trimmed) {
		return true
	}
	if isSchemaStartLine(line, schemaName) {
		*collecting = true
		*lines = append(*lines, trimmed)
		return false
	}
	if !*collecting {
		return false
	}
	if isNextSchemaLine(line, trimmed) {
		return true
	}
	*lines = append(*lines, strings.TrimPrefix(line, "    "))
	return false
}

func isSchemasSectionStart(line, trimmed string) bool {
	return trimmed == "schemas:" && strings.HasPrefix(line, "  ")
}

func isComponentSectionBoundary(line, trimmed string) bool {
	return strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.HasSuffix(trimmed, ":")
}

func isSchemaStartLine(line, schemaName string) bool {
	return strings.HasPrefix(line, "    "+schemaName+":")
}

func isNextSchemaLine(line, trimmed string) bool {
	return strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "      ") && strings.HasSuffix(trimmed, ":")
}

func lookupSchemaBlockForParse(schemaRef string) string {
	if specRaw == nil || schemaRef == "" {
		return ""
	}
	const prefix = "#/components/schemas/"
	name := strings.TrimPrefix(schemaRef, prefix)
	if name == schemaRef || name == "" {
		return ""
	}
	return extractSchemaBlockFull(specRaw, name)
}

func countLeadingSpaces(line string) int {
	count := 0
	for count < len(line) && line[count] == ' ' {
		count++
	}
	return count
}

func placeholderForSchemaType(schemaType string) string {
	switch schemaType {
	case "string":
		return "<string>"
	case "integer", "number":
		return "0"
	case "boolean":
		return "false"
	case "array":
		return "[]"
	case "object":
		return "{}"
	default:
		return "<value>"
	}
}

func parseSchemaRef(schemaRef string) *schemaHint {
	block := lookupSchemaBlockForParse(schemaRef)
	if block == "" {
		return nil
	}
	return parseSchemaBlock(block)
}

func parseSchemaBlock(block string) *schemaHint {
	lines := strings.Split(block, "\n")
	if len(lines) <= 1 {
		return nil
	}
	nodes := parseYAMLNodes(lines[1:])
	return parseSchemaNodes(nodes)
}

func parseYAMLNodes(lines []string) []*yamlNode {
	root := &yamlNode{Indent: -1}
	stack := []*yamlNode{root}

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		node := &yamlNode{
			Indent: countLeadingSpaces(line),
			Text:   strings.TrimSpace(line),
		}
		for len(stack) > 1 && node.Indent <= stack[len(stack)-1].Indent {
			stack = stack[:len(stack)-1]
		}
		parent := stack[len(stack)-1]
		parent.Children = append(parent.Children, node)
		stack = append(stack, node)
	}

	return root.Children
}

func parseSchemaNodes(nodes []*yamlNode) *schemaHint {
	schema := &schemaHint{}
	for _, node := range nodes {
		applySchemaNode(schema, node)
	}
	if schema.Ref == "" && schema.Type == "" && schema.Example == "" && schema.Description == "" && len(schema.Properties) == 0 && schema.Items == nil && len(schema.AllOf) == 0 && len(schema.AnyOf) == 0 {
		return nil
	}
	return schema
}

func applySchemaNode(schema *schemaHint, node *yamlNode) {
	text := strings.TrimPrefix(node.Text, "- ")
	key, value, hasValue := splitYAMLKeyValue(text)

	if key == "description" && applySchemaDescriptionValue(schema, value, node.Children) {
		return
	}
	applySchemaScalarValue(schema, key, value, hasValue)
	if handled := applyStructuredSchemaNode(schema, text, node.Children); handled {
		return
	}
	for _, child := range node.Children {
		applySchemaNode(schema, child)
	}
}

func applySchemaScalarValue(schema *schemaHint, key, value string, hasValue bool) {
	if !hasValue {
		return
	}
	switch key {
	case "$ref":
		schema.Ref = strings.Trim(value, "'\"")
	case "type":
		schema.Type = value
	case "example":
		schema.Example = value
	case "description":
		schema.Description = strings.Trim(value, "'\"")
	default:
		return
	}
}

func applySchemaDescriptionValue(schema *schemaHint, value string, children []*yamlNode) bool {
	if schema == nil || !isDescriptionBlockMarker(value) {
		return false
	}
	schema.Description = collectYAMLBlockText(children)
	return true
}

func isDescriptionBlockMarker(value string) bool {
	switch strings.TrimSpace(value) {
	case ">", ">-", "|", "|-":
		return true
	default:
		return false
	}
}

func collectYAMLBlockText(nodes []*yamlNode) string {
	if len(nodes) == 0 {
		return ""
	}
	parts := make([]string, 0, len(nodes))
	var walk func(items []*yamlNode)
	walk = func(items []*yamlNode) {
		for _, item := range items {
			text := strings.TrimSpace(item.Text)
			if text != "" {
				parts = append(parts, text)
			}
			if len(item.Children) > 0 {
				walk(item.Children)
			}
		}
	}
	walk(nodes)
	return strings.Join(parts, " ")
}

func applyStructuredSchemaNode(schema *schemaHint, text string, children []*yamlNode) bool {
	if !strings.HasSuffix(text, ":") {
		return false
	}
	switch strings.TrimSuffix(text, ":") {
	case "properties":
		applySchemaProperties(schema, children)
	case "items":
		schema.Items = parseSchemaNodes(children)
	case "allOf":
		schema.AllOf = append(schema.AllOf, parseCompositeSchemaChildren(children)...)
	case "anyOf":
		schema.AnyOf = append(schema.AnyOf, parseCompositeSchemaChildren(children)...)
	}
	return true
}

func applySchemaProperties(schema *schemaHint, children []*yamlNode) {
	if schema.Properties == nil {
		schema.Properties = map[string]*schemaHint{}
	}
	for _, child := range children {
		propName := strings.TrimSuffix(child.Text, ":")
		propSchema := parseSchemaNodes(child.Children)
		if propSchema == nil {
			propSchema = &schemaHint{}
		}
		schema.Properties[propName] = propSchema
		schema.PropertyOrder = append(schema.PropertyOrder, propName)
	}
}

func parseCompositeSchemaChildren(children []*yamlNode) []*schemaHint {
	var parsed []*schemaHint
	for _, child := range children {
		if item := parseSchemaNodes([]*yamlNode{child}); item != nil {
			parsed = append(parsed, item)
		}
	}
	return parsed
}

func splitYAMLKeyValue(text string) (string, string, bool) {
	key, value, ok := strings.Cut(text, ":")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(key), strings.TrimSpace(value), true
}

func buildPayloadLines(schema *schemaHint, depth int, seen map[string]bool) []string {
	if schema == nil || depth > 3 {
		return nil
	}

	if resolved := buildPayloadLinesForRef(schema, depth, seen); resolved != nil {
		return resolved
	}

	if lines := buildPayloadLinesForAllOf(schema, depth, seen); len(lines) > 0 {
		return lines
	}
	if lines := buildPayloadLinesForAnyOf(schema, depth, seen); len(lines) > 0 {
		return lines
	}
	if lines := buildPayloadLinesForProperties(schema, depth, seen); len(lines) > 0 {
		return lines
	}
	if lines := buildPayloadLinesForArray(schema, depth, seen); len(lines) > 0 {
		return lines
	}
	if scalar := scalarPayloadValue(schema, depth+1, seen); scalar != "" {
		return []string{scalar}
	}
	return nil
}

func buildPayloadLinesForRef(schema *schemaHint, depth int, seen map[string]bool) []string {
	if schema.Ref == "" {
		return nil
	}
	if seen[schema.Ref] {
		return []string{fmt.Sprintf("<recursive %s>", schema.Ref)}
	}
	seen[schema.Ref] = true
	defer delete(seen, schema.Ref)
	resolved := parseSchemaRef(schema.Ref)
	if resolved == nil {
		return []string{fmt.Sprintf("<see %s>", schema.Ref)}
	}
	return buildPayloadLines(resolved, depth+1, seen)
}

func buildPayloadLinesForAllOf(schema *schemaHint, depth int, seen map[string]bool) []string {
	var lines []string
	for _, item := range schema.AllOf {
		lines = append(lines, buildPayloadLines(item, depth+1, seen)...)
	}
	return lines
}

func buildPayloadLinesForAnyOf(schema *schemaHint, depth int, seen map[string]bool) []string {
	for _, item := range schema.AnyOf {
		if item.Ref == "#/components/schemas/HandlebarsExpression" {
			continue
		}
		if payload := buildPayloadLines(item, depth+1, seen); len(payload) > 0 {
			return payload
		}
	}
	return nil
}

func buildPayloadLinesForProperties(schema *schemaHint, depth int, seen map[string]bool) []string {
	if len(schema.PropertyOrder) == 0 {
		return nil
	}
	var lines []string
	for _, name := range schema.PropertyOrder {
		lines = append(lines, buildPropertyPayloadLines(name, schema.Properties[name], depth, seen)...)
	}
	return lines
}

func buildPropertyPayloadLines(name string, child *schemaHint, depth int, seen map[string]bool) []string {
	if child == nil {
		return nil
	}
	if scalar := scalarPayloadValue(child, depth+1, seen); scalar != "" {
		return []string{fmt.Sprintf("%s: %s", name, scalar)}
	}
	childLines := buildPayloadLines(child, depth+1, seen)
	if len(childLines) == 0 {
		return []string{fmt.Sprintf("%s: <value>", name)}
	}
	lines := []string{name + ":"}
	for _, line := range childLines {
		lines = append(lines, "  "+line)
	}
	return lines
}

func buildPayloadLinesForArray(schema *schemaHint, depth int, seen map[string]bool) []string {
	if schema.Items == nil && schema.Type != "array" {
		return nil
	}
	if scalar := scalarPayloadValue(schema.Items, depth+1, seen); scalar != "" {
		return []string{"- " + scalar}
	}
	childLines := buildPayloadLines(schema.Items, depth+1, seen)
	if len(childLines) == 0 {
		return []string{"- <value>"}
	}
	lines := []string{"-"}
	for _, line := range childLines {
		lines = append(lines, "  "+line)
	}
	return lines
}

func scalarPayloadValue(schema *schemaHint, depth int, seen map[string]bool) string {
	if schema == nil {
		return ""
	}
	if schema.Ref != "" {
		resolved := buildPayloadLines(schema, depth, seen)
		if len(resolved) == 1 && !strings.Contains(resolved[0], ":") && !strings.HasPrefix(resolved[0], "-") {
			return resolved[0]
		}
		return ""
	}
	if schema.Example != "" {
		return schema.Example
	}
	switch schema.Type {
	case "string", "integer", "number", "boolean":
		return placeholderForSchemaType(schema.Type)
	case "array":
		if schema.Items == nil {
			return "[]"
		}
		if item := scalarPayloadValue(schema.Items, depth+1, seen); item != "" {
			return fmt.Sprintf("[%s]", item)
		}
	case "object":
		return ""
	}
	return ""
}
