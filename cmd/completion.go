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

package cmd

import (
	"github.com/spf13/cobra"
)

func configureCompletionCommand(root *cobra.Command) {
	root.InitDefaultCompletionCmd()
}

func isCompletionCmd(cmd *cobra.Command) bool {
	for current := cmd; current != nil; current = current.Parent() {
		if current.Name() == "completion" {
			return true
		}
		if current.Name() == "__complete" || current.Name() == "__completeNoDesc" {
			return true
		}
	}
	return false
}
