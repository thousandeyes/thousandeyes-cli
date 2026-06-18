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
	"strings"
	"testing"
)

func TestCompletionCommandsRunWithoutToken(t *testing.T) {
	t.Setenv("TE_TOKEN", "")
	cases := []struct {
		name                 string
		args                 []string
		forbidInStderrSubstr string
	}{
		{
			name: "completion command",
			args: []string{"completion", "bash"},
		},
		{
			name:                 "hidden complete command",
			args:                 []string{"__complete", "tes"},
			forbidInStderrSubstr: "missing ThousandEyes token",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newRootCommand()
			_, stderr := captureCommandIO(t, cmd)
			cmd.SetArgs(tc.args)

			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute(%v): %v stderr=%q", tc.args, err, stderr.String())
			}
			if tc.forbidInStderrSubstr != "" && strings.Contains(stderr.String(), tc.forbidInStderrSubstr) {
				t.Fatalf("did not expect %q in stderr, got %q", tc.forbidInStderrSubstr, stderr.String())
			}
		})
	}
}
