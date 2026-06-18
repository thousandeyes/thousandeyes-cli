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
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

// SetRootHelpFunc installs grouped root help output.
func SetRootHelpFunc(root *cobra.Command) {
	if root == nil {
		return
	}
	root.SetHelpFunc(rootCommandHelp)
}

func rootCommandHelp(cmd *cobra.Command, _ []string) {
	w := cmd.OutOrStdout()
	if err := rootCommandAPIIndexError(cmd.Root()); err != nil {
		fmt.Fprintf(w, "Warning: %v\n\n", err)
	}
	fmt.Fprintf(w, "Usage:\n  %s\n\n", rootUsageLine(cmd))

	var staticCommands, apiCommands []*cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Hidden {
			continue
		}
		switch sub.Annotations[rootCommandKindAnnotation] {
		case rootCommandKindAPI:
			apiCommands = append(apiCommands, sub)
		default:
			staticCommands = append(staticCommands, sub)
		}
	}

	sortCommandsByName(staticCommands)
	sortCommandsByName(apiCommands)

	if len(staticCommands) > 0 {
		printRootCommandGroup(w, "Static Commands", staticCommands)
	}
	if len(apiCommands) > 0 {
		printRootCommandGroup(w, "API Commands", apiCommands)
	}

	if cmd.HasAvailableLocalFlags() {
		fmt.Fprintln(w, "Global Flags:")
		fmt.Fprint(w, strings.TrimRight(cmd.LocalFlags().FlagUsages(), "\n"))
		fmt.Fprintln(w)
	}
}

func rootUsageLine(cmd *cobra.Command) string {
	base := cmd.CommandPath()
	if base == "" {
		base = cmd.Use
	}
	if hasVisibleSubcommands(cmd) {
		return fmt.Sprintf("%s [command] [flags]", base)
	}
	return cmd.UseLine()
}

func hasVisibleSubcommands(cmd *cobra.Command) bool {
	for _, sub := range cmd.Commands() {
		if !sub.Hidden {
			return true
		}
	}
	return false
}

func sortCommandsByName(commands []*cobra.Command) {
	slices.SortFunc(commands, func(a, b *cobra.Command) int {
		return strings.Compare(a.Name(), b.Name())
	})
}

func printRootCommandGroup(w io.Writer, heading string, commands []*cobra.Command) {
	fmt.Fprintf(w, "%s:\n", heading)
	for _, sub := range commands {
		fmt.Fprintf(w, "  %-20s %s\n", sub.Name(), sub.Short)
	}
	fmt.Fprintln(w)
}
