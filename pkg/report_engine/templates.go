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

package report_engine

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/SENERGY-Platform/reporting-service/lib"
	"github.com/SENERGY-Platform/service-commons/pkg/jwt"
)

// ErrTemplateAuthoringUnavailable means the configured driver cannot write templates.
var ErrTemplateAuthoringUnavailable = errors.New("the report engine driver cannot write templates")

// ErrPreviewData wraps a failure to resolve the data of a preview, such as a figures
// query the caller may not read. Its message is meant for the author.
var ErrPreviewData = errors.New("could not collect the preview data")

func (r *Client) templateDriver() (TemplateAuthoring, error) {
	driver, ok := r.Driver.(TemplateAuthoring)
	if !ok {
		return nil, ErrTemplateAuthoringUnavailable
	}
	return driver, nil
}

// groupOf reads the claim that names the caller's report group from the token. The
// signature is not checked, as everywhere else the token is only read. A missing, empty
// or non-string claim is no group.
func groupOf(authString string, claim string) (string, error) {
	if _, err := jwt.Parse(authString); err != nil {
		return "", fmt.Errorf("%w: %w", lib.ErrUnauthorized, err)
	}
	raw := authString
	if len(raw) > 7 && strings.ToLower(raw[:7]) == "bearer " {
		raw = raw[7:]
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return "", lib.ErrNoReportGroup
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return "", lib.ErrNoReportGroup
	}
	var claims map[string]any
	if err = json.Unmarshal(payload, &claims); err != nil {
		return "", lib.ErrNoReportGroup
	}
	group, _ := claims[claim].(string)
	if strings.TrimSpace(group) == "" {
		return "", lib.ErrNoReportGroup
	}
	return group, nil
}

// templateFolder is the caller's folder: the subfolder of the configured folder named
// like the caller's group claim. Both lookups use the caller's token.
func (r *Client) templateFolder(driver TemplateAuthoring, authString string) (string, error) {
	group, err := groupOf(authString, r.Config.Templates.GroupClaim)
	if err != nil {
		return "", err
	}
	parent, err := driver.FindFolderShortId(r.Config.Templates.Folder, authString)
	if err != nil {
		return "", err
	}
	return driver.FindSubfolderShortId(parent, group, authString)
}

// CreateTemplate creates a template in the caller's group folder. The folder is looked
// up with the caller's token, so a caller who cannot read it gets no template.
func (r *Client) CreateTemplate(input lib.TemplateInput, authString string) (lib.Template, error) {
	driver, err := r.templateDriver()
	if err != nil {
		return lib.Template{}, err
	}
	folder, err := r.templateFolder(driver, authString)
	if err != nil {
		return lib.Template{}, err
	}
	return driver.CreateTemplate(folder, input, authString)
}

// UpdateTemplate replaces a template and its sample data. Only templates in the
// caller's group folder can be changed.
func (r *Client) UpdateTemplate(id string, input lib.TemplateInput, authString string) (lib.Template, error) {
	driver, err := r.templateDriver()
	if err != nil {
		return lib.Template{}, err
	}
	folder, err := r.templateFolder(driver, authString)
	if err != nil {
		return lib.Template{}, err
	}
	return driver.UpdateTemplate(folder, id, input, authString)
}

// DeleteTemplate removes a template and its sample data. Only templates in the
// caller's group folder can be deleted.
func (r *Client) DeleteTemplate(id string, authString string) error {
	driver, err := r.templateDriver()
	if err != nil {
		return err
	}
	folder, err := r.templateFolder(driver, authString)
	if err != nil {
		return err
	}
	return driver.DeleteTemplate(folder, id, authString)
}

// PreviewTemplate renders a draft or a stored template with the data of a report
// model, resolved like a real run, and stores nothing. By default the result is a
// PNG, with asFile it is the file of the template's own recipe.
func (r *Client) PreviewTemplate(request lib.TemplatePreviewRequest, asFile bool, authString string) (content []byte, contentType string, err error) {
	driver, err := r.templateDriver()
	if err != nil {
		return nil, "", err
	}
	var source lib.TemplateSource
	if request.TemplateId != "" {
		if source, err = driver.GetTemplateSource(request.TemplateId, authString); err != nil {
			return nil, "", err
		}
	} else if request.Template != nil {
		source = lib.TemplateSource{Recipe: request.Template.Recipe, Content: request.Template.Content, Helpers: request.Template.Helpers}
	} else {
		return nil, "", fmt.Errorf("%w: neither a template nor a templateId", lib.ErrRejected)
	}
	data, err := r.setReportFileData(request.Data, authString, "preview")
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", ErrPreviewData, err)
	}
	return driver.RenderInline(lib.PreviewRender{TemplateSource: source, Image: !asFile}, data, authString)
}
