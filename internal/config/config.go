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

package config

import (
	"fmt"
	"os"

	"github.com/thousandeyes/thousandeyes-cli/internal/textutil"
)

const defaultBaseURL = "https://api.thousandeyes.com"

type Config struct {
	Token   string
	BaseURL string
}

func LoadWithOverrides(tokenOverride, baseURLOverride string) (Config, error) {
	token := textutil.FirstNonEmpty(tokenOverride, os.Getenv("TE_TOKEN"))
	if token == "" {
		return Config{}, fmt.Errorf("missing ThousandEyes token; pass --token or set TE_TOKEN")
	}

	baseURL := textutil.FirstNonEmpty(baseURLOverride, os.Getenv("TE_BASE_URL"), defaultBaseURL)

	return Config{Token: token, BaseURL: baseURL}, nil
}
