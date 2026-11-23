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
	"errors"
	"fmt"

	"github.com/SENERGY-Platform/reporting-service/lib"
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

// CreateTemplate creates a template in the configured folder. The folder is looked
// up with the caller's token, so a caller who cannot read it gets no template.
func (r *Client) CreateTemplate(input lib.TemplateInput, authString string) (lib.Template, error) {
	driver, err := r.templateDriver()
	if err != nil {
		return lib.Template{}, err
	}
	folder, err := driver.FindFolderShortId(r.Config.Templates.Folder, authString)
	if err != nil {
		return lib.Template{}, err
	}
	return driver.CreateTemplate(folder, input, authString)
}

// UpdateTemplate replaces a template and its sample data. Only templates in the
// configured folder can be changed.
func (r *Client) UpdateTemplate(id string, input lib.TemplateInput, authString string) (lib.Template, error) {
	driver, err := r.templateDriver()
	if err != nil {
		return lib.Template{}, err
	}
	folder, err := driver.FindFolderShortId(r.Config.Templates.Folder, authString)
	if err != nil {
		return lib.Template{}, err
	}
	return driver.UpdateTemplate(folder, id, input, authString)
}

// DeleteTemplate removes a template and its sample data. Only templates in the
// configured folder can be deleted.
func (r *Client) DeleteTemplate(id string, authString string) error {
	driver, err := r.templateDriver()
	if err != nil {
		return err
	}
	folder, err := driver.FindFolderShortId(r.Config.Templates.Folder, authString)
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
