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
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thousandeyes/thousandeyes-cli/internal/apispec"
	"github.com/thousandeyes/thousandeyes-cli/internal/config"
	"github.com/thousandeyes/thousandeyes-cli/internal/textutil"
)

type ConfigProvider func() config.Config

type apiOperation = Operation
type apiParameter = Parameter
type apiRequestBodyContent = RequestBodyContent

// bodyFieldBinding maps a JSON request-body property to a CLI flag for shorthand commands.
type bodyFieldBinding struct {
	JSONKey     string
	FlagName    string
	Kind        string // string, bool, number, json
	Description string
}

// bodyArrayBinding maps repeated CLI values into a top-level JSON array request body.
type bodyArrayBinding struct {
	FlagName    string
	ItemKind    string // string, bool, number, json
	Description string
}

type operationParamBinding struct {
	Name        string
	In          string // path, query
	FlagName    string
	Required    bool
	Description string
}

// Persistent flag names on apiCmd that must not be shadowed by JSON body field flags.
var apiCmdPersistentFlagNames = map[string]struct{}{
	"json": {},
	"help": {},
}

const (
	rootCommandKindAnnotation    = "thousandeyes-cli/root-command-kind"
	rootCommandKindAPI           = "api"
	commandDescriptionAnnotation = "thousandeyes-cli/description"
	apiIndexErrorAnnotation      = "thousandeyes-cli/api-index-error"
)

var loadAPIOperationIndexFn = loadAPIOperationIndex

// RegisterRootResourceCommands attaches all OpenAPI resource commands at root level.
func RegisterRootResourceCommands(root *cobra.Command, getConfig ConfigProvider) {
	if root == nil {
		return
	}

	index, err := loadAPIOperationIndexFn()
	if err != nil {
		setRootCommandAPIIndexError(root, err)
		return
	}
	setRootCommandAPIIndexError(root, nil)

	byResource := groupOperationsByResource(index)
	for _, resKey := range sortedResourceKeys(byResource) {
		root.AddCommand(buildResourceCommand(resKey, byResource[resKey], getConfig))
	}
}

type routedOperation struct {
	operation apiOperation
	segments  []string
}

func groupOperationsByResource(index map[string]apiOperation) map[string][]routedOperation {
	byResource := make(map[string][]routedOperation)
	for _, op := range index {
		segments, ok := apiOperationCommandRoute(op)
		if !ok || len(segments) < 2 {
			continue
		}

		op.Resource = segments[0]
		op.Verb = strings.Join(segments[1:], "-")
		key := normalizeAPIResourceName(segments[0])
		byResource[key] = append(byResource[key], routedOperation{
			operation: op,
			segments:  append([]string(nil), segments...),
		})
	}
	return byResource
}

func sortedResourceKeys(byResource map[string][]routedOperation) []string {
	keys := make([]string, 0, len(byResource))
	for k := range byResource {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func buildResourceCommand(resourceKey string, ops []routedOperation, getConfig ConfigProvider) *cobra.Command {
	sortRoutedOperations(ops)
	parent := newAPIResourceParentCommand(resourceKey)
	setRootCommandKindAnnotation(parent, rootCommandKindAPI)
	for _, routed := range ops {
		addRoutedOperation(parent, routed, getConfig)
	}
	return parent
}

func sortRoutedOperations(ops []routedOperation) {
	slices.SortFunc(ops, func(a, b routedOperation) int {
		aPath := strings.Join(a.segments, "/")
		bPath := strings.Join(b.segments, "/")
		if cmp := strings.Compare(aPath, bPath); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.operation.ID, b.operation.ID)
	})
}

func addRoutedOperation(parent *cobra.Command, routed routedOperation, getConfig ConfigProvider) {
	current := parent
	for i := 1; i < len(routed.segments)-1; i++ {
		current = ensureAPINamespaceCommand(current, routed.segments[i])
	}

	use := apiAllocateLeafUse(current, routed.operation, routed.segments[len(routed.segments)-1])
	verb := apiVerbCommand(current, getConfig, routed.operation, use)
	verb.SetHelpFunc(apiVerbCommandHelp)
	current.AddCommand(verb)
}

func setRootCommandAPIIndexError(root *cobra.Command, err error) {
	if root == nil {
		return
	}
	if err == nil {
		if root.Annotations != nil {
			delete(root.Annotations, apiIndexErrorAnnotation)
		}
		return
	}
	setCommandAnnotation(root, apiIndexErrorAnnotation, err.Error())
}

func rootCommandAPIIndexError(root *cobra.Command) error {
	if root == nil || root.Annotations == nil {
		return nil
	}
	msg := strings.TrimSpace(root.Annotations[apiIndexErrorAnnotation])
	if msg == "" {
		return nil
	}
	return fmt.Errorf("load API command index: %s", msg)
}

// RootResourceRegistrationError reports startup-time API command registration failures.
func RootResourceRegistrationError(root *cobra.Command) error {
	return rootCommandAPIIndexError(root)
}

func newAPIResourceParentCommand(resource string) *cobra.Command {
	parent := newAPIHelpOnlyCommand(resource)
	parent.PersistentFlags().Bool("json", false, "Print raw API payload as JSON")
	return parent
}

func ensureAPINamespaceCommand(parent *cobra.Command, use string) *cobra.Command {
	for _, sub := range parent.Commands() {
		if sub.Name() == use {
			return sub
		}
	}

	group := newAPIHelpOnlyCommand(use)
	parent.AddCommand(group)
	return group
}

func newAPIHelpOnlyCommand(use string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: "",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			return cmd.Help()
		},
	}
	cmd.SetHelpFunc(apiResourceParentHelp)
	return cmd
}

func apiAllocateLeafUse(parent *cobra.Command, op apiOperation, preferred string) string {
	preferred = strings.TrimSpace(preferred)
	if preferred == "" {
		preferred = op.Verb
	}
	if !commandHasChild(parent, preferred) {
		return preferred
	}

	fallback := strings.TrimSpace(apiVerbUseDisambiguated(op))
	if fallback != "" && !commandHasChild(parent, fallback) {
		return fallback
	}

	base := preferred
	if fallback != "" {
		base = fallback
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !commandHasChild(parent, candidate) {
			return candidate
		}
	}
}

func commandHasChild(parent *cobra.Command, name string) bool {
	for _, sub := range parent.Commands() {
		if sub.Name() == name {
			return true
		}
	}
	return false
}

func normalizeAPIResourceName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, " ", "-")
	return value
}

func apiOperationCommandRoute(op apiOperation) ([]string, bool) {
	if len(op.CommandPath) > 0 {
		copied := append([]string(nil), op.CommandPath...)
		return copied, true
	}
	return nil, false
}

func setRootCommandKindAnnotation(cmd *cobra.Command, kind string) {
	setCommandAnnotation(cmd, rootCommandKindAnnotation, kind)
}

func setCommandAnnotation(cmd *cobra.Command, key, value string) {
	if cmd == nil || key == "" || value == "" {
		return
	}
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[key] = value
}

func apiVerbCommand(apiCmd *cobra.Command, getConfig ConfigProvider, op apiOperation, use string) *cobra.Command {
	var paramBindings []operationParamBinding
	var bodyBindings []bodyFieldBinding
	var bodyArrayBinding *bodyArrayBinding
	description := textutil.FirstNonEmpty(strings.TrimSpace(op.Description), strings.TrimSpace(op.Summary))

	c := &cobra.Command{
		Use:   use,
		Args:  cobra.NoArgs,
		Short: textutil.FirstNonEmpty(strings.TrimSpace(op.Summary), description),
		Long:  describeOperationText(op),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			return runAPISingleOperation(cmd, getConfig(), op, paramBindings, bodyBindings, bodyArrayBinding)
		},
	}
	setCommandAnnotation(c, commandDescriptionAnnotation, description)

	paramBindings = buildOperationParamBindings(c, apiCmd, op.Parameters)
	registerOperationParamFlags(c, paramBindings)

	bodyBindings = buildBodyFieldBindings(apiCmd, c, op)
	registerBodyFieldFlags(c, bodyBindings)
	bodyArrayBinding = buildBodyArrayBinding(apiCmd, c, op)
	registerBodyArrayFlag(c, bodyArrayBinding)
	return c
}

func apiVerbUseDisambiguated(op apiOperation) string {
	id := op.ID
	verb := op.Verb
	lowerID := strings.ToLower(id)
	if strings.HasPrefix(lowerID, verb) {
		id = id[len(verb):]
	}
	if id == "" {
		return op.ID
	}
	parts := apispec.SplitCamelCase(id)
	if len(parts) == 0 {
		return op.ID
	}
	var b strings.Builder
	b.WriteString(verb)
	for _, p := range parts {
		b.WriteString("-")
		b.WriteString(strings.ToLower(p))
	}
	return b.String()
}
