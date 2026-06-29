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

type Operation struct {
	ID            string
	Verb          string
	Resource      string
	CommandPath   []string
	CommandSource string
	Method        string
	Path          string
	Summary       string
	Description   string
	Parameters    []Parameter
	HasBody       bool
	RequestBody   []RequestBodyContent
}

type Parameter struct {
	Name        string
	In          string
	Description string
	Required    bool
}

type RequestBodyContent struct {
	ContentType string
	SchemaRef   string
	SchemaType  string
	ItemsType   string
}

const (
	CommandSourceCLICommand = "x-thousandeyes-cli-command"
)

type Property struct {
	Name        string
	Kind        string
	Description string
	Required    bool
}

type schemaHint struct {
	Ref           string
	Type          string
	Example       string
	Description   string
	Properties    map[string]*schemaHint
	PropertyOrder []string
	Required      []string
	Items         *schemaHint
	AllOf         []*schemaHint
	AnyOf         []*schemaHint
}

type yamlNode struct {
	Indent   int
	Text     string
	Children []*yamlNode
}
