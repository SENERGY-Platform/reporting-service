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
	"testing"

	"github.com/SENERGY-Platform/reporting-service/lib"
	"github.com/SENERGY-Platform/reporting-service/pkg/config"
)

// previewDriver is a driver that can render inline and records what it was given.
type previewDriver struct {
	fakeDriver
	source   lib.TemplateSource
	rendered []lib.PreviewRender
	data     map[string]interface{}
}

func (d *previewDriver) FindFolderShortId(string, string) (string, error) { return "f", nil }
func (d *previewDriver) CreateTemplate(string, lib.TemplateInput, string) (lib.Template, error) {
	return lib.Template{}, nil
}
func (d *previewDriver) UpdateTemplate(string, string, lib.TemplateInput, string) (lib.Template, error) {
	return lib.Template{}, nil
}
func (d *previewDriver) DeleteTemplate(string, string, string) error { return nil }
func (d *previewDriver) GetTemplateSource(string, string) (lib.TemplateSource, error) {
	return d.source, nil
}
func (d *previewDriver) RenderInline(render lib.PreviewRender, data map[string]interface{}, _ string) ([]byte, string, error) {
	d.rendered = append(d.rendered, render)
	d.data = data
	return []byte("PNG"), "image/png", nil
}

func previewClient(driver ReportingDriver) *Client {
	return &Client{Driver: driver, Config: &config.Config{}}
}

func previewToken() string {
	// a token without a role is enough here, the client does not check roles
	return "Bearer eyJhbGciOiJub25lIn0.eyJzdWIiOiJ1MSJ9.sig"
}

func TestPreviewFillsTheDataLikeARunAndStoresNothing(t *testing.T) {
	driver := &previewDriver{}
	client := previewClient(driver)

	content, contentType, err := client.PreviewTemplate(lib.TemplatePreviewRequest{
		Template: &lib.TemplatePreviewDraft{Recipe: "chrome-pdf", Content: "x"},
		Data: map[string]lib.ReportObject{
			"title": {DataType: lib.DataType{Name: "title", ValueType: "string"}, Value: "Hallo"},
		},
	}, false, previewToken())
	if err != nil {
		t.Fatal(err)
	}

	if string(content) != "PNG" || contentType != "image/png" {
		t.Errorf("answer = %q %q", content, contentType)
	}
	if driver.data["title"] != "Hallo" {
		t.Errorf("data = %v, want the report object resolved", driver.data)
	}
	if len(driver.rendered) != 1 || !driver.rendered[0].Image {
		t.Errorf("rendered = %+v, want one image render", driver.rendered)
	}
	if len(driver.renders()) != 0 {
		t.Error("a preview must not create a report file")
	}
}

func TestPreviewResolvesAFiguresQuery(t *testing.T) {
	driver := &previewDriver{}
	client := previewClient(driver)

	_, _, err := client.PreviewTemplate(lib.TemplatePreviewRequest{
		Template: &lib.TemplatePreviewDraft{Recipe: "chrome-pdf", Content: "x"},
		Data: map[string]lib.ReportObject{
			"consumption": {DataType: lib.DataType{Name: "consumption", ValueType: "object"}, FiguresQuery: &lib.FiguresQuery{
				GraphId: "g", Period: lib.FiguresPeriod{Kind: "previousMonth"},
			}},
		},
	}, false, previewToken())

	// no figures service is configured, which only a resolved figuresQuery notices
	if !errors.Is(err, ErrPreviewData) {
		t.Fatalf("err = %v, want the preview data error", err)
	}
	if len(driver.rendered) != 0 {
		t.Error("nothing may be rendered without the data")
	}
}

func TestPreviewOfAStoredTemplateRendersItsSourceInTheRequestedForm(t *testing.T) {
	driver := &previewDriver{source: lib.TemplateSource{Recipe: "html-to-xlsx", Content: "stored"}}
	client := previewClient(driver)

	_, _, err := client.PreviewTemplate(lib.TemplatePreviewRequest{TemplateId: "t1"}, true, previewToken())
	if err != nil {
		t.Fatal(err)
	}

	if len(driver.rendered) != 1 || driver.rendered[0].Image || driver.rendered[0].Recipe != "html-to-xlsx" || driver.rendered[0].Content != "stored" {
		t.Errorf("rendered = %+v, want the stored source as a file", driver.rendered)
	}
}

func TestDriverWithoutTemplateAuthoringIsReported(t *testing.T) {
	client := previewClient(&fakeDriver{})

	_, err := client.CreateTemplate(lib.TemplateInput{Name: "n"}, previewToken())

	if !errors.Is(err, ErrTemplateAuthoringUnavailable) {
		t.Fatalf("err = %v, want the unavailable error", err)
	}
}

func TestCreateTemplateLooksTheFolderUpByTheConfiguredName(t *testing.T) {
	driver := &folderDriver{}
	client := &Client{Driver: driver, Config: &config.Config{Templates: config.TemplateConfig{Folder: "custom_reports", AuthorRole: "r"}}}

	if _, err := client.CreateTemplate(lib.TemplateInput{Name: "n"}, previewToken()); err != nil {
		t.Fatal(err)
	}

	if driver.looked != "custom_reports" || driver.createdIn != "folder-short" {
		t.Errorf("looked up %q, created in %q", driver.looked, driver.createdIn)
	}
}

type folderDriver struct {
	previewDriver
	looked    string
	createdIn string
	updatedIn string
	deletedIn string
}

func (d *folderDriver) UpdateTemplate(folder string, _ string, _ lib.TemplateInput, _ string) (lib.Template, error) {
	d.updatedIn = folder
	return lib.Template{}, nil
}
func (d *folderDriver) DeleteTemplate(folder string, _ string, _ string) error {
	d.deletedIn = folder
	return nil
}

// A change is checked against the folder named in the configuration, so it is looked up first.
func TestUpdateAndDeleteAreCheckedAgainstTheConfiguredFolder(t *testing.T) {
	driver := &folderDriver{}
	client := &Client{Driver: driver, Config: &config.Config{Templates: config.TemplateConfig{Folder: "custom_reports", AuthorRole: "r"}}}

	if _, err := client.UpdateTemplate("t1", lib.TemplateInput{Name: "n"}, previewToken()); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteTemplate("t1", previewToken()); err != nil {
		t.Fatal(err)
	}

	if driver.looked != "custom_reports" || driver.updatedIn != "folder-short" || driver.deletedIn != "folder-short" {
		t.Errorf("looked up %q, updated in %q, deleted in %q", driver.looked, driver.updatedIn, driver.deletedIn)
	}
}

func (d *folderDriver) FindFolderShortId(name string, _ string) (string, error) {
	d.looked = name
	return "folder-short", nil
}
func (d *folderDriver) CreateTemplate(folder string, _ lib.TemplateInput, _ string) (lib.Template, error) {
	d.createdIn = folder
	return lib.Template{}, nil
}
