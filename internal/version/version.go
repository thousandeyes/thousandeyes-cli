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

// Package version holds build metadata injected via -ldflags at release time.
package version

var (
	// Version is the release tag (e.g. v1.2.3) or "dev" for local builds.
	Version = "dev"
	// Commit is the git SHA at build time.
	Commit = "none"
	// Date is the build timestamp (RFC3339).
	Date = "unknown"
)

// UserAgent returns the product/version token sent with API requests.
func UserAgent() string {
	return "ThousandEyesCLI/" + Version
}
