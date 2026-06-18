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
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

func describeOperationText(op apiOperation) string {
	var b strings.Builder
	fmt.Fprintln(&b, "API Details:")
	fmt.Fprintf(&b, "  command: %s %s\n", op.Resource, op.Verb)
	fmt.Fprintf(&b, "  method:  %s\n", op.Method)
	fmt.Fprintf(&b, "  path:    %s\n", op.Path)
	if op.ID != "" {
		fmt.Fprintf(&b, "  operationId: %s\n", op.ID)
	}
	if op.HasBody {
		fmt.Fprintf(&b, "  request body: %s\n", operationBodyContentTypes(op.RequestBody))
	} else {
		b.WriteString("  request body: none\n")
	}
	return b.String()
}

func operationBodyContentTypes(bodies []apiRequestBodyContent) string {
	contentTypes := make([]string, 0, len(bodies))
	for _, body := range bodies {
		if body.ContentType == "" {
			continue
		}
		contentTypes = append(contentTypes, body.ContentType)
	}
	if len(contentTypes) == 0 {
		return "supported"
	}
	return strings.Join(contentTypes, ", ")
}

func isJSONContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return strings.Contains(contentType, "json")
}

func printHumanReadableJSON(w io.Writer, value any) error {
	return printHumanReadableValueWithIndent(w, value, "", newHumanReadableStyler(w))
}

func printHumanReadableValueWithIndent(w io.Writer, value any, indent string, styler humanReadableStyler) error {
	switch v := value.(type) {
	case map[string]any:
		return printHumanReadableObject(w, v, indent, styler)
	case []any:
		return printHumanReadableArray(w, v, indent, styler)
	default:
		_, err := fmt.Fprintf(w, "%s%s\n", indent, formatHumanReadableValue(v))
		return err
	}
}

func printHumanReadableObject(w io.Writer, obj map[string]any, indent string, styler humanReadableStyler) error {
	keys := orderedObjectKeys(obj)
	return printHumanReadableObjectWithKeys(w, obj, keys, indent, styler)
}

func printHumanReadableObjectWithKeys(w io.Writer, obj map[string]any, keys []string, indent string, styler humanReadableStyler) error {
	for _, key := range keys {
		value := obj[key]
		keyLabel := styler.key(key)
		if isScalarJSONValue(value) {
			formatted := formatHumanReadableValue(value)
			if _, err := fmt.Fprintf(w, "%s%s: %s\n", indent, keyLabel, formatted); err != nil {
				return err
			}
			continue
		}

		if _, err := fmt.Fprintf(w, "%s%s:\n", indent, keyLabel); err != nil {
			return err
		}
		if err := printHumanReadableValueWithIndent(w, value, indent+"  ", styler); err != nil {
			return err
		}
	}
	return nil
}

func printHumanReadableArray(w io.Writer, items []any, indent string, styler humanReadableStyler) error {
	if len(items) == 0 {
		_, err := fmt.Fprintf(w, "%s[]\n", indent)
		return err
	}

	for _, item := range items {
		if err := printHumanReadableArrayItem(w, item, indent, styler); err != nil {
			return err
		}
	}
	return nil
}

func printHumanReadableArrayItem(w io.Writer, item any, indent string, styler humanReadableStyler) error {
	switch v := item.(type) {
	case map[string]any:
		return printHumanReadableArrayObjectItem(w, v, indent, styler)
	case []any:
		return printHumanReadableArrayNestedItem(w, v, indent, styler)
	default:
		return printHumanReadableArrayScalarItem(w, v, indent)
	}
}

func printHumanReadableArrayObjectItem(w io.Writer, obj map[string]any, indent string, styler humanReadableStyler) error {
	keys := orderedObjectKeys(obj)
	if len(keys) == 0 {
		_, err := fmt.Fprintf(w, "%s- {}\n", indent)
		return err
	}

	if err := printHumanReadableArrayObjectFirstKey(w, obj, keys[0], indent, styler); err != nil {
		return err
	}
	if len(keys) > 1 {
		return printHumanReadableObjectWithKeys(w, obj, keys[1:], indent+"  ", styler)
	}
	return nil
}

func printHumanReadableArrayObjectFirstKey(w io.Writer, obj map[string]any, key, indent string, styler humanReadableStyler) error {
	value := obj[key]
	keyLabel := styler.key(key)
	if isScalarJSONValue(value) {
		_, err := fmt.Fprintf(w, "%s- %s: %s\n", indent, keyLabel, formatHumanReadableValue(value))
		return err
	}
	if _, err := fmt.Fprintf(w, "%s- %s:\n", indent, keyLabel); err != nil {
		return err
	}
	return printHumanReadableValueWithIndent(w, value, indent+"    ", styler)
}

func printHumanReadableArrayNestedItem(w io.Writer, items []any, indent string, styler humanReadableStyler) error {
	if _, err := fmt.Fprintf(w, "%s-\n", indent); err != nil {
		return err
	}
	return printHumanReadableArray(w, items, indent+"  ", styler)
}

func printHumanReadableArrayScalarItem(w io.Writer, value any, indent string) error {
	_, err := fmt.Fprintf(w, "%s- %s\n", indent, formatHumanReadableValue(value))
	return err
}

func isScalarJSONValue(value any) bool {
	switch value.(type) {
	case nil, string, bool, float64:
		return true
	default:
		return false
	}
}

func formatHumanReadableValue(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case string:
		if shouldQuoteHumanReadableString(v) {
			return strconv.Quote(v)
		}
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Sprintf("%v", v)
		}
		if math.Trunc(v) == v && v <= math.MaxInt64 && v >= math.MinInt64 {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(raw)
	}
}

func orderedObjectKeys(obj map[string]any) []string {
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func shouldQuoteHumanReadableString(s string) bool {
	if s == "" {
		return true
	}
	if strings.TrimSpace(s) != s {
		return true
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
		switch r {
		case '{', '}', '[', ']', '"':
			return true
		}
	}
	return false
}

type humanReadableStyler struct {
	colorEnabled bool
}

func newHumanReadableStyler(w io.Writer) humanReadableStyler {
	return humanReadableStyler{colorEnabled: shouldUseANSIColor(w)}
}

func shouldUseANSIColor(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("CLICOLOR") == "0" {
		return false
	}
	if strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func formatHelpSectionHeader(w io.Writer, header string) string {
	if !shouldUseANSIColor(w) {
		return header
	}
	return "\x1b[1m" + header + "\x1b[0m"
}

func (s humanReadableStyler) key(value string) string {
	if !s.colorEnabled {
		return value
	}
	return "\x1b[36m" + value + "\x1b[0m"
}

var apiSharedFlagNames = []string{"help", "token", "base-url", "json"}

func sharedAPIFlagUsages(cmd *cobra.Command) string {
	if cmd == nil {
		return ""
	}
	cmd.InitDefaultHelpFlag()
	set := pflag.NewFlagSet("shared-api", pflag.ContinueOnError)
	set.SortFlags = true
	for _, flagName := range apiSharedFlagNames {
		if flag := cmd.Flags().Lookup(flagName); flag != nil {
			set.AddFlag(flag)
		}
	}
	return strings.TrimRight(set.FlagUsages(), "\n")
}

func operationFlagUsages(cmd *cobra.Command) string {
	if cmd == nil {
		return ""
	}
	excluded := map[string]struct{}{}
	for _, flagName := range apiSharedFlagNames {
		excluded[flagName] = struct{}{}
	}
	set := pflag.NewFlagSet("operation", pflag.ContinueOnError)
	set.SortFlags = true
	cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
		if _, isExcluded := excluded[flag.Name]; isExcluded {
			return
		}
		set.AddFlag(flag)
	})
	return strings.TrimRight(set.FlagUsages(), "\n")
}

func apiResourceParentHelp(cmd *cobra.Command, _ []string) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%s\n  %s\n\n", formatHelpSectionHeader(w, "Usage:"), apiResourceUsageLine(cmd))
	var actions, subResources []*cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Hidden {
			continue
		}
		if hasVisibleSubcommands(sub) {
			subResources = append(subResources, sub)
			continue
		}
		actions = append(actions, sub)
	}
	if len(actions) > 0 {
		fmt.Fprintf(w, "%s\n", formatHelpSectionHeader(w, "Actions:"))
		for _, sub := range actions {
			fmt.Fprintf(w, "  %-14s %s\n", sub.Name(), sub.Short)
		}
	}
	if len(subResources) > 0 {
		if len(actions) > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s\n", formatHelpSectionHeader(w, "Sub-resources:"))
		for _, sub := range subResources {
			fmt.Fprintf(w, "  %-14s %s\n", sub.Name(), sub.Short)
		}
	}
	if sharedUsages := sharedAPIFlagUsages(cmd); sharedUsages != "" {
		fmt.Fprintf(w, "\n%s\n", formatHelpSectionHeader(w, "Shared API flags:"))
		fmt.Fprintln(w, sharedUsages)
	}
}

func apiResourceUsageLine(cmd *cobra.Command) string {
	if cmd == nil {
		return ""
	}
	base := cmd.CommandPath()
	if base == "" {
		base = cmd.Use
	}
	if hasVisibleSubcommands(cmd) {
		return fmt.Sprintf("%s [action|sub-resource] [flags]", base)
	}
	return cmd.UseLine()
}

func apiVerbCommandHelp(cmd *cobra.Command, _ []string) {
	w := cmd.OutOrStdout()
	description := strings.TrimSpace(cmd.Annotations[commandDescriptionAnnotation])
	if description == "" {
		description = strings.TrimSpace(cmd.Short)
	}
	if description != "" {
		fmt.Fprintln(w, description)
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "%s\n  %s\n\n", formatHelpSectionHeader(w, "Usage:"), cmd.UseLine())
	if operationUsages := operationFlagUsages(cmd); operationUsages != "" {
		fmt.Fprintf(w, "%s\n", formatHelpSectionHeader(w, "Flags:"))
		fmt.Fprint(w, operationUsages)
		fmt.Fprintln(w)
		fmt.Fprintln(w)
	}
	if sharedUsages := sharedAPIFlagUsages(cmd); sharedUsages != "" {
		fmt.Fprintln(w, formatHelpSectionHeader(w, "Shared API flags:"))
		fmt.Fprintln(w, sharedUsages)
	}
}
