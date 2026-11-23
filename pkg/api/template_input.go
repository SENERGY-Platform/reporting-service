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

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/SENERGY-Platform/reporting-service/lib"
	"github.com/gin-gonic/gin"
)

const (
	// maxTemplateName leaves room for the suffix of the sample data's name.
	maxTemplateName    = 100
	maxTemplateContent = 512 << 10
	maxTemplateHelpers = 64 << 10
	maxSampleData      = 256 << 10
	// maxTemplateBody bounds what is read at all, before any field is looked at.
	maxTemplateBody = 2 << 20
)

// templateRecipes are the recipes an author may choose. The engine is fixed to handlebars.
var templateRecipes = map[string]bool{"chrome-pdf": true, "html-to-xlsx": true}

// invalidNameChars are the characters jsreport refuses in an entity name.
const invalidNameChars = `<>:"/\|?*`

// decodeBody reads a bounded JSON body. With strict it refuses fields it does not
// know, so an engine or a script in a template body is an error and not ignored.
func decodeBody(c *gin.Context, target any, strict bool) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxTemplateBody)
	decoder := json.NewDecoder(c.Request.Body)
	if strict {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("unexpected data after the json body")
	}
	return nil
}

func validateRecipe(recipe string) error {
	if !templateRecipes[recipe] {
		return fmt.Errorf("recipe must be chrome-pdf or html-to-xlsx, got %q", recipe)
	}
	return nil
}

func validateContent(content string, helpers string) error {
	if strings.TrimSpace(content) == "" {
		return errors.New("content must not be empty")
	}
	if len(content) > maxTemplateContent {
		return fmt.Errorf("content is larger than %d bytes", maxTemplateContent)
	}
	if len(helpers) > maxTemplateHelpers {
		return fmt.Errorf("helpers are larger than %d bytes", maxTemplateHelpers)
	}
	return nil
}

func validateTemplateName(name string) error {
	if name != strings.TrimSpace(name) || name == "" {
		return errors.New("name must not be empty or start or end with a space")
	}
	if utf8.RuneCountInString(name) > maxTemplateName {
		return fmt.Errorf("name is longer than %d characters", maxTemplateName)
	}
	if name == "." || name == ".." {
		return errors.New("name must not be . or ..")
	}
	for _, r := range name {
		if unicode.IsControl(r) || strings.ContainsRune(invalidNameChars, r) {
			return fmt.Errorf("name must not contain %q", r)
		}
	}
	return nil
}

// validateTemplateInput checks a create or update body at the edge, before jsreport sees it.
func validateTemplateInput(input lib.TemplateInput) error {
	if err := validateTemplateName(input.Name); err != nil {
		return err
	}
	if err := validateRecipe(input.Recipe); err != nil {
		return err
	}
	if err := validateContent(input.Content, input.Helpers); err != nil {
		return err
	}
	raw, err := json.Marshal(input.SampleData)
	if err != nil {
		return fmt.Errorf("sampleData is not valid json: %w", err)
	}
	if len(raw) > maxSampleData {
		return fmt.Errorf("sampleData is larger than %d bytes", maxSampleData)
	}
	return nil
}

// validatePreviewRequest wants exactly one of a draft and a stored template's id.
func validatePreviewRequest(request lib.TemplatePreviewRequest) error {
	if (request.Template == nil) == (request.TemplateId == "") {
		return errors.New("give either template or templateId")
	}
	if request.Template == nil {
		return nil
	}
	if err := validateRecipe(request.Template.Recipe); err != nil {
		return err
	}
	return validateContent(request.Template.Content, request.Template.Helpers)
}
