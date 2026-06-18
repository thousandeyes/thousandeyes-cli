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

package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/thousandeyes/thousandeyes-cli/internal/apispec"
)

type missingOperation struct {
	ID     string
	Method string
	Path   string
}

func main() {
	strict := flag.Bool("strict", false, "fail when operations are missing x-thousandeyes-cli-command")
	flag.Parse()

	index, err := apispec.LoadOperationIndex()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to load API operation index: %v\n", err)
		os.Exit(1)
	}

	missing := collectMissingOperations(index)
	if len(missing) == 0 {
		fmt.Fprintln(os.Stdout, "OK: every API operation has x-thousandeyes-cli-command coverage.")
		return
	}

	fmt.Fprintf(os.Stderr, "WARN: %d API operations are missing x-thousandeyes-cli-command and are not exposed as shorthand commands.\n", len(missing))
	for _, op := range missing {
		fmt.Fprintf(os.Stderr, "WARN: missing x-thousandeyes-cli-command for %s %s (operationId=%s)\n", op.Method, op.Path, op.ID)
	}

	if *strict || strings.EqualFold(strings.TrimSpace(os.Getenv("TE_CLI_COMMAND_STRICT")), "1") {
		os.Exit(2)
	}
}

func collectMissingOperations(index map[string]apispec.Operation) []missingOperation {
	missing := make([]missingOperation, 0)
	for _, op := range index {
		if len(op.CommandPath) > 0 {
			continue
		}
		missing = append(missing, missingOperation{
			ID:     op.ID,
			Method: op.Method,
			Path:   op.Path,
		})
	}

	sort.Slice(missing, func(i, j int) bool {
		if missing[i].Path != missing[j].Path {
			return missing[i].Path < missing[j].Path
		}
		if missing[i].Method != missing[j].Method {
			return missing[i].Method < missing[j].Method
		}
		return missing[i].ID < missing[j].ID
	})
	return missing
}
