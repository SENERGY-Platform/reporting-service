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

package jsreport

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/SENERGY-Platform/reporting-service/lib"
	"github.com/go-resty/resty/v2"
)

const (
	templateEngine = "handlebars"
	// A preview image shows the top of the page in a fixed viewport, not the whole page.
	previewViewportWidth  = 1200
	previewViewportHeight = 1600
	dataNameSuffix        = " data"
)

// APIError is a failed answer of jsreport with the message it carried. It unwraps
// to the lib error that matches its status, so a handler can pick a status of its own.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("jsreport-api: status %d: %s", e.Status, e.Message)
}

func (e *APIError) Unwrap() error {
	switch {
	// jsreport reports a taken name as 500, so the message decides, not the status
	case strings.Contains(e.Message, "already exists"):
		return lib.ErrConflict
	case e.Status == http.StatusForbidden:
		return lib.ErrForbidden
	case e.Status == http.StatusNotFound:
		return lib.ErrNotFound
	case e.Status == http.StatusBadRequest || e.Status == http.StatusUnprocessableEntity:
		return lib.ErrRejected
	}
	return nil
}

// checkWriteResponse is checkResponse for the authoring calls: it keeps the status and
// the message of jsreport instead of reducing them to one string.
func checkWriteResponse(response *resty.Response) error {
	if response.IsSuccess() {
		return nil
	}
	if response.StatusCode() == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	var body ErrorResponse
	_ = json.Unmarshal(response.Body(), &body)
	message := body.Error.Message
	if message == "" {
		message = body.Message
	}
	return &APIError{Status: response.StatusCode(), Message: message}
}

func odataQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// FindFolderShortId looks a folder up by name with the caller's token. A folder the
// caller may not read is not listed by jsreport, so it is reported as not readable.
func (j *Client) FindFolderShortId(name string, authString string) (string, error) {
	response, err := j.HttpClient.R().SetHeader("Authorization", authString).
		Get(j.BaseUrl + "/odata/folders?$select=name,shortid,folder&$filter=" + url.QueryEscape("name eq "+odataQuote(name)))
	if err != nil {
		return "", err
	}
	if err = checkWriteResponse(response); err != nil {
		return "", err
	}
	var resp FolderResponse
	if err = json.Unmarshal(response.Body(), &resp); err != nil {
		return "", err
	}
	// a name is unique per parent only, so a top level folder wins over a nested one
	found := ""
	for _, folder := range resp.Folders {
		if folder.ShortId == "" {
			continue
		}
		if folder.Parent.ShortId == "" {
			return folder.ShortId, nil
		}
		if found == "" {
			found = folder.ShortId
		}
	}
	if found == "" {
		return "", fmt.Errorf("%w: %q", lib.ErrTemplateFolderAccess, name)
	}
	return found, nil
}

func dataName(templateName string) string {
	return templateName + dataNameSuffix
}

// templateBodyOf is the body that writes a stored template back as it was.
func templateBodyOf(stored Template) map[string]interface{} {
	return map[string]interface{}{
		"name": stored.Name, "engine": stored.Engine, "recipe": stored.Recipe,
		"content": stored.Content, "helpers": stored.Helpers,
	}
}

func templateBody(input lib.TemplateInput) map[string]interface{} {
	body := map[string]interface{}{
		"name":    input.Name,
		"engine":  templateEngine,
		"recipe":  input.Recipe,
		"content": input.Content,
		"helpers": input.Helpers,
	}
	return body
}

func sampleDataJSON(input lib.TemplateInput) (string, error) {
	sample := input.SampleData
	if sample == nil {
		sample = map[string]interface{}{}
	}
	raw, err := json.Marshal(sample)
	return string(raw), err
}

// CreateTemplate stores the sample data and the template in the folder with the
// caller's token and links the two. jsreport adds the caller to the permissions of both.
func (j *Client) CreateTemplate(folderShortId string, input lib.TemplateInput, authString string) (template lib.Template, err error) {
	dataJSON, err := sampleDataJSON(input)
	if err != nil {
		return
	}
	if err = j.assertNamesFree(folderShortId, []string{input.Name, dataName(input.Name)}, nil, authString); err != nil {
		return
	}
	createdData, err := j.insertData(input.Name, dataJSON, folderShortId, authString)
	if err != nil {
		return
	}
	body := templateBody(input)
	body["data"] = map[string]string{"shortid": createdData.ShortId}
	body["folder"] = map[string]string{"shortid": folderShortId}
	raw, err := j.insert("templates", body, authString)
	if err != nil {
		// a template that was not stored must not leave its data behind
		return template, j.withCleanup(err, j.remove("data", createdData.Id, authString), "its sample data "+createdData.Id)
	}
	var created Template
	if err = json.Unmarshal(raw, &created); err != nil {
		return
	}
	return buildTemplate(created, createdData, dataJSON)
}

// UpdateTemplate replaces the template and its sample data. Both checks come first,
// that the template lies in the folder and that the new names are free, so a refusal
// changes nothing. If the second write fails the first is written back.
func (j *Client) UpdateTemplate(folderShortId string, id string, input lib.TemplateInput, authString string) (template lib.Template, err error) {
	stored, err := j.getTemplateInFolder(id, folderShortId, authString)
	if err != nil {
		return
	}
	oldData, err := j.dataOf(stored, authString)
	if err != nil {
		return
	}
	// a data entity outside the folder is not ours to change, the template gets a new one
	dataOwned := oldData.Id != "" && oldData.Folder.ShortId == folderShortId
	ignore := []string{stored.Id}
	if dataOwned {
		ignore = append(ignore, oldData.Id)
	}
	if err = j.assertNamesFree(folderShortId, []string{input.Name, dataName(input.Name)}, ignore, authString); err != nil {
		return
	}
	dataJSON, err := sampleDataJSON(input)
	if err != nil {
		return
	}
	body := templateBody(input)
	var data Data
	if !dataOwned {
		if data, err = j.insertData(input.Name, dataJSON, folderShortId, authString); err != nil {
			return
		}
		body["data"] = map[string]string{"shortid": data.ShortId}
		if err = j.patch("templates", id, body, authString); err != nil {
			return template, j.withCleanup(err, j.remove("data", data.Id, authString), "its new sample data "+data.Id)
		}
	} else {
		if err = j.patch("templates", id, body, authString); err != nil {
			return
		}
		data = oldData
		if err = j.patch("data", oldData.Id, map[string]interface{}{"name": dataName(input.Name), "dataJson": dataJSON}, authString); err != nil {
			return template, j.withCleanup(err, j.patch("templates", id, templateBodyOf(stored), authString), "the template "+id)
		}
		data.Name = dataName(input.Name)
	}
	stored.Name = input.Name
	stored.Recipe = input.Recipe
	return buildTemplate(stored, data, dataJSON)
}

// DeleteTemplate removes a template in the folder and then its sample data, which
// stays if it lies elsewhere.
func (j *Client) DeleteTemplate(folderShortId string, id string, authString string) error {
	stored, err := j.getTemplateInFolder(id, folderShortId, authString)
	if err != nil {
		return err
	}
	data, err := j.dataOf(stored, authString)
	if err != nil {
		return err
	}
	if err = j.remove("templates", id, authString); err != nil {
		return err
	}
	if data.Id == "" || data.Folder.ShortId != folderShortId {
		return nil
	}
	if err = j.remove("data", data.Id, authString); err != nil && !errors.Is(err, lib.ErrNotFound) {
		return fmt.Errorf("template %s deleted, its sample data was not: %w", id, err)
	}
	return nil
}

// GetTemplateSource reads what a preview of a stored template renders from.
func (j *Client) GetTemplateSource(id string, authString string) (lib.TemplateSource, error) {
	stored, err := j.getTemplate(id, authString)
	if err != nil {
		return lib.TemplateSource{}, err
	}
	return lib.TemplateSource{ShortId: stored.ShortId, Recipe: stored.Recipe, Content: stored.Content, Helpers: stored.Helpers}, nil
}

// RenderInline renders without saving a report. A stored template is rendered by
// its shortid, so its engine, scripts and chrome settings apply as in a real run,
// and without data jsreport falls back to its sample data. An image is the top of
// the page as PNG in a fixed viewport, otherwise the file of the recipe.
func (j *Client) RenderInline(render lib.PreviewRender, data map[string]interface{}, authString string) ([]byte, string, error) {
	template := map[string]interface{}{}
	if render.ShortId != "" {
		template["shortid"] = render.ShortId
	} else {
		template["content"] = render.Content
		template["engine"] = templateEngine
		template["recipe"] = render.Recipe
		template["helpers"] = render.Helpers
	}
	if render.Image {
		template["recipe"] = "chrome-image"
		// chrome-image reads chromeImage, not chrome
		template["chromeImage"] = map[string]interface{}{
			"type":           "png",
			"fullPage":       false,
			"viewportWidth":  previewViewportWidth,
			"viewportHeight": previewViewportHeight,
		}
	}
	body := map[string]interface{}{
		"template": template,
		"options":  map[string]interface{}{"reports": map[string]interface{}{"save": false}},
	}
	// an empty object would replace the sample data, absent data does not
	if len(data) > 0 {
		body["data"] = data
	}
	response, err := j.HttpClient.R().SetHeader("Authorization", authString).SetBody(body).Post(j.BaseUrl + "/api/report")
	if err != nil {
		return nil, "", err
	}
	if err = checkWriteResponse(response); err != nil {
		return nil, "", err
	}
	return response.Body(), response.Header().Get("Content-Type"), nil
}

// getTemplate reads a template by id. jsreport answers an id it does not know, or
// one the caller may not read, with an empty list and status 200.
func (j *Client) getTemplate(id string, authString string) (template Template, err error) {
	response, err := j.HttpClient.R().SetHeader("Authorization", authString).Get(j.BaseUrl + "/odata/templates(" + url.PathEscape(odataQuote(id)) + ")")
	if err != nil {
		return
	}
	if err = checkWriteResponse(response); err != nil {
		return
	}
	if err = json.Unmarshal(response.Body(), &template); err != nil {
		return
	}
	if template.Id == "" {
		return template, fmt.Errorf("%w: template %s", lib.ErrNotFound, id)
	}
	return
}

func (j *Client) getTemplateInFolder(id string, folderShortId string, authString string) (Template, error) {
	template, err := j.getTemplate(id, authString)
	if err != nil {
		return template, err
	}
	if template.Folder.ShortId != folderShortId {
		return template, fmt.Errorf("%w: template %s", lib.ErrOutsideTemplateFolder, id)
	}
	return template, nil
}

// dataOf reads the data entity a template points at, empty if it has none.
func (j *Client) dataOf(template Template, authString string) (Data, error) {
	if template.Data.ShortId == "" {
		return Data{}, nil
	}
	return j.getTemplateDataByShortId(template.Data.ShortId, authString)
}

func (j *Client) insertData(templateName string, dataJSON string, folderShortId string, authString string) (data Data, err error) {
	raw, err := j.insert("data", map[string]interface{}{
		"name":     dataName(templateName),
		"dataJson": dataJSON,
		"folder":   map[string]string{"shortid": folderShortId},
	}, authString)
	if err != nil {
		return
	}
	err = json.Unmarshal(raw, &data)
	return
}

// assertNamesFree refuses a name that a template or data entity in the folder already
// carries. jsreport compares names case-insensitively and across entity sets, and
// answers a duplicate with a 500, so the check is made before anything is written.
func (j *Client) assertNamesFree(folderShortId string, names []string, ignoreIds []string, authString string) error {
	for _, set := range []string{"templates", "data"} {
		response, err := j.HttpClient.R().SetHeader("Authorization", authString).
			Get(j.BaseUrl + "/odata/" + set + "?$select=name&$filter=" + url.QueryEscape("folder/shortid eq "+odataQuote(folderShortId)))
		if err != nil {
			return err
		}
		if err = checkWriteResponse(response); err != nil {
			return err
		}
		var resp NamedEntityResponse
		if err = json.Unmarshal(response.Body(), &resp); err != nil {
			return err
		}
		for _, entity := range resp.Entities {
			if slices.Contains(ignoreIds, entity.Id) {
				continue
			}
			for _, name := range names {
				if strings.EqualFold(entity.Name, name) {
					return &APIError{Status: http.StatusConflict, Message: fmt.Sprintf("Entity with name %q already exists in the template folder", name)}
				}
			}
		}
	}
	return nil
}

// withCleanup returns err and, if the cleanup after it failed too, says what was left.
func (j *Client) withCleanup(err error, cleanupErr error, left string) error {
	if cleanupErr == nil {
		return err
	}
	return fmt.Errorf("%w (and %s could not be restored: %v)", err, left, cleanupErr)
}

func buildTemplate(template Template, data Data, dataJSON string) (result lib.Template, err error) {
	result.Id = template.Id
	result.Name = template.Name
	result.Type = TypeMap[template.Recipe]
	result.Data.Id = data.Id
	result.Data.Name = data.Name
	result.Data.DataJSONString = dataJSON
	var raw map[string]interface{}
	if err = json.Unmarshal([]byte(dataJSON), &raw); err != nil {
		return
	}
	result.Data.DataStructured = getJsonKeysAndTypes(raw)
	return
}

func (j *Client) insert(set string, body map[string]interface{}, authString string) ([]byte, error) {
	response, err := j.HttpClient.R().SetHeader("Authorization", authString).SetBody(body).Post(j.BaseUrl + "/odata/" + set)
	if err != nil {
		return nil, err
	}
	if err = checkWriteResponse(response); err != nil {
		return nil, err
	}
	return response.Body(), nil
}

func (j *Client) patch(set string, id string, body map[string]interface{}, authString string) error {
	response, err := j.HttpClient.R().SetHeader("Authorization", authString).SetBody(body).
		Patch(j.BaseUrl + "/odata/" + set + "(" + url.PathEscape(odataQuote(id)) + ")")
	if err != nil {
		return err
	}
	return checkWriteResponse(response)
}

func (j *Client) remove(set string, id string, authString string) error {
	response, err := j.HttpClient.R().SetHeader("Authorization", authString).
		Delete(j.BaseUrl + "/odata/" + set + "(" + url.PathEscape(odataQuote(id)) + ")")
	if err != nil {
		return err
	}
	return checkWriteResponse(response)
}
