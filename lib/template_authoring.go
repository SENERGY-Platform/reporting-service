/*
 * Copyright 2026 InfAI (CC SES)
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package lib

import "errors"

// Errors of the report engine that a handler turns into a status of its own. They
// are wrapped around the engine's message, so the message still reaches the log.
var (
	ErrForbidden            = errors.New("report engine denied access")
	ErrNotFound             = errors.New("not found in the report engine")
	ErrConflict             = errors.New("already exists in the report engine")
	ErrRejected             = errors.New("report engine rejected the input")
	ErrTemplateFolderAccess = errors.New("template folder not readable")
	// ErrOutsideTemplateFolder means the template exists but not in the configured folder.
	ErrOutsideTemplateFolder = errors.New("only templates in the template folder can be changed")
)

// TemplateInput is what an author sends to create or replace a template. The
// engine is always handlebars and the template carries no scripts or assets.
type TemplateInput struct {
	Name    string `json:"name"`
	Recipe  string `json:"recipe"`
	Content string `json:"content"`
	// Helpers is the JavaScript of the template's handlebars helpers.
	Helpers string `json:"helpers,omitempty"`
	// SampleData is stored as the template's data entity and shown in the editor.
	SampleData map[string]interface{} `json:"sampleData"`
}

// TemplateSource is the part of a stored template a preview renders from.
type TemplateSource struct {
	// ShortId lets a preview render the stored template itself, with its engine,
	// scripts and chrome settings, instead of a copy of its text.
	ShortId string
	Recipe  string
	Content string
	Helpers string
}

// PreviewRender asks the engine to render a template that is not stored.
type PreviewRender struct {
	TemplateSource
	// Image renders a PNG of the top of the page at a fixed viewport instead of the recipe's file.
	Image bool
}

// TemplatePreviewRequest is the body of a template preview: either a draft in
// Template or the id of a stored template, plus the report data to fill it with.
type TemplatePreviewRequest struct {
	Template   *TemplatePreviewDraft   `json:"template,omitempty"`
	TemplateId string                  `json:"templateId,omitempty"`
	Data       map[string]ReportObject `json:"data,omitempty"`
}

// TemplatePreviewDraft is a template that exists only in the request.
type TemplatePreviewDraft struct {
	Recipe  string `json:"recipe"`
	Content string `json:"content"`
	Helpers string `json:"helpers,omitempty"`
}
