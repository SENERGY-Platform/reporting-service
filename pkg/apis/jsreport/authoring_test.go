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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/SENERGY-Platform/reporting-service/lib"
	"github.com/go-resty/resty/v2"
)

type recordedCall struct {
	Method string
	Path   string
	Query  string
	Auth   string
	Body   map[string]any
}

// recordingJSReport answers by "METHOD path" from replies and records every call.
type recordingJSReport struct {
	mu      sync.Mutex
	calls   []recordedCall
	replies map[string]reply
}

type reply struct {
	status int
	body   string
}

func newRecordingJSReport(t *testing.T, replies map[string]reply) (*Client, *recordingJSReport) {
	t.Helper()
	rec := &recordingJSReport{replies: replies}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		call := recordedCall{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Auth: r.Header.Get("Authorization")}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &call.Body)
		}
		rec.mu.Lock()
		rec.calls = append(rec.calls, call)
		rec.mu.Unlock()
		answer, ok := rec.replies[r.Method+" "+r.URL.Path]
		if !ok {
			answer = reply{status: http.StatusTeapot, body: `{"error":{"message":"unexpected call"}}`}
		}
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(answer.status)
		_, _ = w.Write([]byte(answer.body))
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseUrl: srv.URL, HttpClient: resty.New()}, rec
}

func (r *recordingJSReport) all(method string, path string) []recordedCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	var found []recordedCall
	for _, call := range r.calls {
		if call.Method == method && call.Path == path {
			found = append(found, call)
		}
	}
	return found
}

// writes counts the calls that change something in jsreport.
func (r *recordingJSReport) writes() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, call := range r.calls {
		if call.Method != http.MethodGet {
			count++
		}
	}
	return count
}

func (r *recordingJSReport) find(method string, path string) *recordedCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.calls {
		if r.calls[i].Method == method && r.calls[i].Path == path {
			return &r.calls[i]
		}
	}
	return nil
}

func sampleInput() lib.TemplateInput {
	return lib.TemplateInput{
		Name:       "Verbrauch",
		Recipe:     "chrome-pdf",
		Content:    "<h1>{{title}}</h1>",
		Helpers:    "function up(s){return s.toUpperCase()}",
		SampleData: map[string]interface{}{"title": "x"},
	}
}

// The answers below are what jsreport 4.15.0 gives: an entity by id is the entity
// itself, an unknown or unreadable id is an empty list with status 200, and a list
// of names comes as {"value": [...]}.
const (
	noEntities  = `{"value":[]}`
	storedT1    = `{"_id":"t1","shortid":"tshort","name":"Alt","engine":"handlebars","recipe":"chrome-pdf","content":"old","helpers":"oldh","data":{"shortid":"dshort"},"folder":{"shortid":"fshort"}}`
	storedData  = `{"value":[{"_id":"d1","shortid":"dshort","name":"Alt data","dataJson":"{}","folder":{"shortid":"fshort"}}]}`
	listEntries = `{"value":[{"_id":"other","name":"Taken"},{"_id":"t1","name":"Alt"},{"_id":"d1","name":"Alt data"}]}`
)

func TestCreateTemplateLinksDataAndFolderWithTheCallersToken(t *testing.T) {
	client, rec := newRecordingJSReport(t, map[string]reply{
		"GET /odata/templates":  {http.StatusOK, noEntities},
		"GET /odata/data":       {http.StatusOK, noEntities},
		"POST /odata/data":      {http.StatusCreated, `{"_id":"d1","shortid":"dshort","name":"Verbrauch data"}`},
		"POST /odata/templates": {http.StatusCreated, `{"_id":"t1","shortid":"tshort","name":"Verbrauch","recipe":"chrome-pdf"}`},
	})

	template, err := client.CreateTemplate("fshort", sampleInput(), "Bearer caller")
	if err != nil {
		t.Fatal(err)
	}

	data := rec.find(http.MethodPost, "/odata/data")
	if data == nil || data.Auth != "Bearer caller" {
		t.Fatalf("data call = %+v, want the caller's token", data)
	}
	if data.Body["name"] != "Verbrauch data" || data.Body["dataJson"] != `{"title":"x"}` {
		t.Errorf("data body = %v", data.Body)
	}
	if folder, _ := data.Body["folder"].(map[string]any); folder["shortid"] != "fshort" {
		t.Errorf("data folder = %v, want fshort", data.Body["folder"])
	}
	tpl := rec.find(http.MethodPost, "/odata/templates")
	if tpl == nil || tpl.Auth != "Bearer caller" {
		t.Fatalf("template call = %+v, want the caller's token", tpl)
	}
	if tpl.Body["engine"] != "handlebars" || tpl.Body["recipe"] != "chrome-pdf" || tpl.Body["content"] != "<h1>{{title}}</h1>" {
		t.Errorf("template body = %v", tpl.Body)
	}
	if link, _ := tpl.Body["data"].(map[string]any); link["shortid"] != "dshort" {
		t.Errorf("template data link = %v, want dshort", tpl.Body["data"])
	}
	if folder, _ := tpl.Body["folder"].(map[string]any); folder["shortid"] != "fshort" {
		t.Errorf("template folder = %v, want fshort", tpl.Body["folder"])
	}
	if _, ok := tpl.Body["scripts"]; ok {
		t.Error("a template must not carry scripts")
	}
	if template.Id != "t1" || template.Type != "PDF" || template.Data.Id != "d1" || template.Data.DataJSONString != `{"title":"x"}` {
		t.Errorf("template = %+v", template)
	}
}

func TestCreateTemplateRemovesItsDataWhenTheTemplateIsRefused(t *testing.T) {
	client, rec := newRecordingJSReport(t, map[string]reply{
		"GET /odata/templates":     {http.StatusOK, noEntities},
		"GET /odata/data":          {http.StatusOK, noEntities},
		"POST /odata/data":         {http.StatusCreated, `{"_id":"d1","shortid":"dshort"}`},
		"POST /odata/templates":    {http.StatusBadRequest, `{"error":{"message":"invalid helpers"}}`},
		"DELETE /odata/data('d1')": {http.StatusNoContent, ``},
	})

	_, err := client.CreateTemplate("fshort", sampleInput(), "Bearer caller")

	if !errors.Is(err, lib.ErrRejected) {
		t.Fatalf("err = %v, want the engine's refusal", err)
	}
	if rec.find(http.MethodDelete, "/odata/data('d1')") == nil {
		t.Error("the sample data of the refused template was left behind")
	}
}

// jsreport answers a taken name with a 500 and the message only, so the message is
// what identifies it. The name is also checked before anything is written.
func TestATakenNameIsAConflictWhateverTheStatus(t *testing.T) {
	client, rec := newRecordingJSReport(t, map[string]reply{
		"GET /odata/templates":     {http.StatusOK, noEntities},
		"GET /odata/data":          {http.StatusOK, noEntities},
		"POST /odata/data":         {http.StatusCreated, `{"_id":"d1","shortid":"dshort"}`},
		"POST /odata/templates":    {http.StatusInternalServerError, `{"error":{"message":"Entity with name \"Verbrauch\" already exists in the /custom folder."}}`},
		"DELETE /odata/data('d1')": {http.StatusNoContent, ``},
	})

	_, err := client.CreateTemplate("fshort", sampleInput(), "Bearer caller")

	if !errors.Is(err, lib.ErrConflict) {
		t.Fatalf("err = %v, want a conflict", err)
	}
	if rec.find(http.MethodDelete, "/odata/data('d1')") == nil {
		t.Error("the sample data was left behind")
	}
}

func TestCreateTemplateRefusesATakenNameBeforeWriting(t *testing.T) {
	cases := map[string]map[string]reply{
		"template name":         {"GET /odata/templates": {http.StatusOK, `{"value":[{"_id":"x","name":"VERBRAUCH"}]}`}, "GET /odata/data": {http.StatusOK, noEntities}},
		"data name":             {"GET /odata/templates": {http.StatusOK, noEntities}, "GET /odata/data": {http.StatusOK, `{"value":[{"_id":"x","name":"verbrauch DATA"}]}`}},
		"name of a data entity": {"GET /odata/templates": {http.StatusOK, noEntities}, "GET /odata/data": {http.StatusOK, `{"value":[{"_id":"x","name":"Verbrauch"}]}`}},
	}
	for name, replies := range cases {
		t.Run(name, func(t *testing.T) {
			client, rec := newRecordingJSReport(t, replies)

			_, err := client.CreateTemplate("fshort", sampleInput(), "Bearer caller")

			if !errors.Is(err, lib.ErrConflict) {
				t.Fatalf("err = %v, want a conflict", err)
			}
			if rec.writes() != 0 {
				t.Errorf("%d writes were made, want none", rec.writes())
			}
			list := rec.find(http.MethodGet, "/odata/templates")
			if list == nil || !strings.Contains(list.Query, "fshort") {
				t.Errorf("list call = %+v, want it limited to the folder", list)
			}
		})
	}
}

func TestUpdateTemplatePatchesTemplateAndData(t *testing.T) {
	client, rec := newRecordingJSReport(t, map[string]reply{
		"GET /odata/templates('t1')":   {http.StatusOK, storedT1},
		"GET /odata/data":              {http.StatusOK, storedData},
		"GET /odata/templates":         {http.StatusOK, listEntries},
		"PATCH /odata/data('d1')":      {http.StatusNoContent, ``},
		"PATCH /odata/templates('t1')": {http.StatusNoContent, ``},
	})
	input := sampleInput()
	input.Recipe = "html-to-xlsx"

	template, err := client.UpdateTemplate("fshort", "t1", input, "Bearer caller")
	if err != nil {
		t.Fatal(err)
	}

	tpl := rec.find(http.MethodPatch, "/odata/templates('t1')")
	if tpl == nil || tpl.Auth != "Bearer caller" || tpl.Body["recipe"] != "html-to-xlsx" || tpl.Body["name"] != "Verbrauch" || tpl.Body["engine"] != "handlebars" {
		t.Errorf("template patch = %+v", tpl)
	}
	data := rec.find(http.MethodPatch, "/odata/data('d1')")
	if data == nil || data.Auth != "Bearer caller" || data.Body["dataJson"] != `{"title":"x"}` || data.Body["name"] != "Verbrauch data" {
		t.Errorf("data patch = %+v", data)
	}
	if template.Type != "Excel" || template.Name != "Verbrauch" || template.Data.Name != "Verbrauch data" {
		t.Errorf("template = %+v", template)
	}
}

func TestUpdateTemplateKeepingItsOwnNameIsNotAConflict(t *testing.T) {
	client, _ := newRecordingJSReport(t, map[string]reply{
		"GET /odata/templates('t1')":   {http.StatusOK, storedT1},
		"GET /odata/data":              {http.StatusOK, storedData},
		"GET /odata/templates":         {http.StatusOK, listEntries},
		"PATCH /odata/data('d1')":      {http.StatusNoContent, ``},
		"PATCH /odata/templates('t1')": {http.StatusNoContent, ``},
	})
	input := sampleInput()
	input.Name = "Alt"

	if _, err := client.UpdateTemplate("fshort", "t1", input, "Bearer caller"); err != nil {
		t.Fatalf("err = %v, want the template's own name to be free for itself", err)
	}
}

func TestUpdateTemplateWithATakenNameChangesNothing(t *testing.T) {
	client, rec := newRecordingJSReport(t, map[string]reply{
		"GET /odata/templates('t1')": {http.StatusOK, storedT1},
		"GET /odata/data":            {http.StatusOK, storedData},
		"GET /odata/templates":       {http.StatusOK, listEntries},
	})
	input := sampleInput()
	input.Name = "taken"

	_, err := client.UpdateTemplate("fshort", "t1", input, "Bearer caller")

	if !errors.Is(err, lib.ErrConflict) {
		t.Fatalf("err = %v, want a conflict", err)
	}
	if rec.writes() != 0 {
		t.Errorf("%d writes were made, want none", rec.writes())
	}
}

func TestUpdateTemplateWritesTheTemplateBackWhenTheDataIsRefused(t *testing.T) {
	client, rec := newRecordingJSReport(t, map[string]reply{
		"GET /odata/templates('t1')":   {http.StatusOK, storedT1},
		"GET /odata/data":              {http.StatusOK, storedData},
		"GET /odata/templates":         {http.StatusOK, noEntities},
		"PATCH /odata/templates('t1')": {http.StatusNoContent, ``},
		"PATCH /odata/data('d1')":      {http.StatusForbidden, `{"error":{"message":"Permission denied"}}`},
	})

	_, err := client.UpdateTemplate("fshort", "t1", sampleInput(), "Bearer caller")

	if !errors.Is(err, lib.ErrForbidden) {
		t.Fatalf("err = %v, want the refusal of the data", err)
	}
	patches := rec.all(http.MethodPatch, "/odata/templates('t1')")
	if len(patches) != 2 {
		t.Fatalf("%d template patches, want the change and its reversal", len(patches))
	}
	back := patches[1].Body
	if back["name"] != "Alt" || back["content"] != "old" || back["helpers"] != "oldh" || back["recipe"] != "chrome-pdf" || back["engine"] != "handlebars" {
		t.Errorf("reversal = %v, want the stored state", back)
	}
}

func TestUpdateTemplateGivesATemplateWithForeignDataANewDataEntity(t *testing.T) {
	foreign := `{"value":[{"_id":"d1","shortid":"dshort","name":"Alt data","dataJson":"{}","folder":{"shortid":"elsewhere"}}]}`
	client, rec := newRecordingJSReport(t, map[string]reply{
		"GET /odata/templates('t1')":   {http.StatusOK, storedT1},
		"GET /odata/data":              {http.StatusOK, foreign},
		"GET /odata/templates":         {http.StatusOK, noEntities},
		"POST /odata/data":             {http.StatusCreated, `{"_id":"d2","shortid":"newshort"}`},
		"PATCH /odata/templates('t1')": {http.StatusNoContent, ``},
	})

	_, err := client.UpdateTemplate("fshort", "t1", sampleInput(), "Bearer caller")
	if err != nil {
		t.Fatal(err)
	}

	if rec.find(http.MethodPatch, "/odata/data('d1')") != nil {
		t.Error("a data entity outside the folder must not be changed")
	}
	created := rec.find(http.MethodPost, "/odata/data")
	if folder, _ := created.Body["folder"].(map[string]any); folder["shortid"] != "fshort" {
		t.Errorf("new data folder = %v, want fshort", created.Body["folder"])
	}
	patch := rec.find(http.MethodPatch, "/odata/templates('t1')")
	if link, _ := patch.Body["data"].(map[string]any); link["shortid"] != "newshort" {
		t.Errorf("template data link = %v, want the new entity", patch.Body["data"])
	}
}

func TestUpdateRemovesTheNewDataWhenTheTemplateCannotBeRelinked(t *testing.T) {
	foreign := `{"value":[{"_id":"d1","shortid":"dshort","folder":{"shortid":"elsewhere"}}]}`
	client, rec := newRecordingJSReport(t, map[string]reply{
		"GET /odata/templates('t1')":   {http.StatusOK, storedT1},
		"GET /odata/data":              {http.StatusOK, foreign},
		"GET /odata/templates":         {http.StatusOK, noEntities},
		"POST /odata/data":             {http.StatusCreated, `{"_id":"d2","shortid":"newshort"}`},
		"PATCH /odata/templates('t1')": {http.StatusForbidden, `{"error":{"message":"Permission denied"}}`},
		"DELETE /odata/data('d2')":     {http.StatusNoContent, ``},
	})

	_, err := client.UpdateTemplate("fshort", "t1", sampleInput(), "Bearer caller")

	if !errors.Is(err, lib.ErrForbidden) {
		t.Fatalf("err = %v, want the refusal", err)
	}
	if rec.find(http.MethodDelete, "/odata/data('d2')") == nil {
		t.Error("the new data entity was left behind")
	}
}

func TestUpdateAndDeleteRefuseATemplateOutsideTheFolder(t *testing.T) {
	outside := `{"_id":"t1","shortid":"tshort","name":"Alt","folder":{"shortid":"elsewhere"},"data":{"shortid":"dshort"}}`
	for name, call := range map[string]func(c *Client) error{
		"update": func(c *Client) error {
			_, err := c.UpdateTemplate("fshort", "t1", sampleInput(), "Bearer caller")
			return err
		},
		"delete": func(c *Client) error { return c.DeleteTemplate("fshort", "t1", "Bearer caller") },
	} {
		t.Run(name, func(t *testing.T) {
			client, rec := newRecordingJSReport(t, map[string]reply{"GET /odata/templates('t1')": {http.StatusOK, outside}})

			err := call(client)

			if !errors.Is(err, lib.ErrOutsideTemplateFolder) {
				t.Fatalf("err = %v, want outside the template folder", err)
			}
			if rec.writes() != 0 {
				t.Errorf("%d writes were made, want none", rec.writes())
			}
		})
	}
}

func TestATemplateWithoutAFolderIsOutsideTheTemplateFolder(t *testing.T) {
	client, _ := newRecordingJSReport(t, map[string]reply{"GET /odata/templates('t1')": {http.StatusOK, `{"_id":"t1","name":"Root"}`}})

	err := client.DeleteTemplate("fshort", "t1", "Bearer caller")

	if !errors.Is(err, lib.ErrOutsideTemplateFolder) {
		t.Fatalf("err = %v, want outside the template folder", err)
	}
}

// An unknown id and one the caller may not read both come back as an empty list.
func TestAnEmptyAnswerForAnIdIsNotFound(t *testing.T) {
	empty := map[string]reply{"GET /odata/templates('nope')": {http.StatusOK, `{"@odata.context":"x","value":[]}`}}
	for name, call := range map[string]func(c *Client) error{
		"update": func(c *Client) error {
			_, err := c.UpdateTemplate("fshort", "nope", sampleInput(), "Bearer caller")
			return err
		},
		"delete": func(c *Client) error { return c.DeleteTemplate("fshort", "nope", "Bearer caller") },
		"source": func(c *Client) error {
			_, err := c.GetTemplateSource("nope", "Bearer caller")
			return err
		},
		"get by id": func(c *Client) error {
			_, err := c.GetTemplateById("nope", "Bearer caller")
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			client, rec := newRecordingJSReport(t, empty)

			err := call(client)

			if !errors.Is(err, lib.ErrNotFound) {
				t.Fatalf("err = %v, want not found", err)
			}
			if rec.writes() != 0 {
				t.Errorf("%d writes were made, want none", rec.writes())
			}
		})
	}
}

func TestDeleteTemplateRemovesTemplateThenData(t *testing.T) {
	client, rec := newRecordingJSReport(t, map[string]reply{
		"GET /odata/templates('t1')":    {http.StatusOK, storedT1},
		"GET /odata/data":               {http.StatusOK, storedData},
		"DELETE /odata/templates('t1')": {http.StatusNoContent, ``},
		"DELETE /odata/data('d1')":      {http.StatusNoContent, ``},
	})

	if err := client.DeleteTemplate("fshort", "t1", "Bearer caller"); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/odata/templates('t1')", "/odata/data('d1')"} {
		call := rec.find(http.MethodDelete, path)
		if call == nil || call.Auth != "Bearer caller" {
			t.Errorf("delete %s = %+v, want a call with the caller's token", path, call)
		}
	}
}

func TestDeleteTemplateKeepsDataOutsideTheFolder(t *testing.T) {
	foreign := `{"value":[{"_id":"d1","shortid":"dshort","folder":{"shortid":"elsewhere"}}]}`
	client, rec := newRecordingJSReport(t, map[string]reply{
		"GET /odata/templates('t1')":    {http.StatusOK, storedT1},
		"GET /odata/data":               {http.StatusOK, foreign},
		"DELETE /odata/templates('t1')": {http.StatusNoContent, ``},
	})

	if err := client.DeleteTemplate("fshort", "t1", "Bearer caller"); err != nil {
		t.Fatal(err)
	}

	if rec.find(http.MethodDelete, "/odata/data('d1')") != nil {
		t.Error("a data entity outside the folder must stay")
	}
}

func TestFindFolderPrefersATopLevelFolder(t *testing.T) {
	client, rec := newRecordingJSReport(t, map[string]reply{
		"GET /odata/folders": {http.StatusOK, `{"value":[{"shortid":"nested","name":"o'brien","folder":{"shortid":"p"}},{"shortid":"top","name":"o'brien"}]}`},
	})

	got, err := client.FindFolderShortId("o'brien", "Bearer caller")
	if err != nil {
		t.Fatal(err)
	}

	if got != "top" {
		t.Errorf("folder = %q, want top", got)
	}
	call := rec.find(http.MethodGet, "/odata/folders")
	if call.Auth != "Bearer caller" || !strings.Contains(call.Query, "o%27%27brien") {
		t.Errorf("call = %+v, want the token and the quote doubled in the filter", call)
	}
}

func TestFindFolderNotListedIsNotReadable(t *testing.T) {
	client, _ := newRecordingJSReport(t, map[string]reply{
		"GET /odata/folders": {http.StatusOK, `{"value":[]}`},
	})

	_, err := client.FindFolderShortId("custom", "Bearer caller")

	if !errors.Is(err, lib.ErrTemplateFolderAccess) {
		t.Fatalf("err = %v, want template folder not readable", err)
	}
}

func TestRenderInlineImageUsesChromeImageInAFixedViewport(t *testing.T) {
	client, rec := newRecordingJSReport(t, map[string]reply{
		"POST /api/report": {http.StatusOK, `PNGDATA`},
	})

	content, contentType, err := client.RenderInline(lib.PreviewRender{
		TemplateSource: lib.TemplateSource{Recipe: "chrome-pdf", Content: "<p/>", Helpers: "h"},
		Image:          true,
	}, map[string]interface{}{"a": 1}, "Bearer caller")
	if err != nil {
		t.Fatal(err)
	}

	if string(content) != "PNGDATA" || contentType != "image/png" {
		t.Errorf("answer = %q %q", content, contentType)
	}
	call := rec.find(http.MethodPost, "/api/report")
	template, _ := call.Body["template"].(map[string]any)
	if template["recipe"] != "chrome-image" || template["engine"] != "handlebars" || template["content"] != "<p/>" {
		t.Errorf("template = %v", template)
	}
	image, _ := template["chromeImage"].(map[string]any)
	if image["viewportWidth"] != float64(1200) || image["viewportHeight"] != float64(1600) || image["fullPage"] != false {
		t.Errorf("chromeImage = %v, want the top of the page in 1200 by 1600", image)
	}
	reports, _ := call.Body["options"].(map[string]any)["reports"].(map[string]any)
	if reports["save"] != false {
		t.Errorf("options = %v, want the report not saved", call.Body["options"])
	}
	if data, _ := call.Body["data"].(map[string]any); data["a"] != float64(1) {
		t.Errorf("data = %v", call.Body["data"])
	}
}

func TestRenderInlineAsFileKeepsTheRecipe(t *testing.T) {
	client, rec := newRecordingJSReport(t, map[string]reply{
		"POST /api/report": {http.StatusOK, `XLSX`},
	})

	_, _, err := client.RenderInline(lib.PreviewRender{
		TemplateSource: lib.TemplateSource{Recipe: "html-to-xlsx", Content: "<table/>"},
	}, nil, "Bearer caller")
	if err != nil {
		t.Fatal(err)
	}

	template, _ := rec.find(http.MethodPost, "/api/report").Body["template"].(map[string]any)
	if template["recipe"] != "html-to-xlsx" {
		t.Errorf("recipe = %v, want html-to-xlsx", template["recipe"])
	}
	if _, ok := template["chromeImage"]; ok {
		t.Error("a file render must not carry image options")
	}
}

// A stored template is rendered by its shortid so that its engine, scripts and chrome
// settings apply, and only the recipe is overridden for an image.
func TestRenderStoredTemplateByShortIdWithoutACopyOfItsText(t *testing.T) {
	source := lib.TemplateSource{ShortId: "tshort", Recipe: "chrome-pdf", Content: "stored", Helpers: "h"}
	for name, tc := range map[string]struct {
		image  bool
		recipe any
	}{"image": {true, "chrome-image"}, "file": {false, nil}} {
		t.Run(name, func(t *testing.T) {
			client, rec := newRecordingJSReport(t, map[string]reply{"POST /api/report": {http.StatusOK, `X`}})

			_, _, err := client.RenderInline(lib.PreviewRender{TemplateSource: source, Image: tc.image}, nil, "Bearer caller")
			if err != nil {
				t.Fatal(err)
			}

			template, _ := rec.find(http.MethodPost, "/api/report").Body["template"].(map[string]any)
			if template["shortid"] != "tshort" || template["recipe"] != tc.recipe {
				t.Errorf("template = %v, want shortid and recipe %v", template, tc.recipe)
			}
			for _, key := range []string{"content", "helpers", "engine"} {
				if _, ok := template[key]; ok {
					t.Errorf("template carries %s, want the stored one to apply", key)
				}
			}
		})
	}
}

// An empty data object would replace the sample data of a stored template, an absent one does not.
func TestRenderWithoutDataSendsNoDataKey(t *testing.T) {
	client, rec := newRecordingJSReport(t, map[string]reply{"POST /api/report": {http.StatusOK, `X`}})

	_, _, err := client.RenderInline(lib.PreviewRender{TemplateSource: lib.TemplateSource{ShortId: "tshort"}}, map[string]interface{}{}, "Bearer caller")
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := rec.find(http.MethodPost, "/api/report").Body["data"]; ok {
		t.Error("an empty data object was sent, which hides the sample data")
	}
}

func TestWriteErrorsKeepStatusAndMessage(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		want   error
	}{
		"forbidden": {http.StatusForbidden, `{"error":{"message":"Permission denied"}}`, lib.ErrForbidden},
		"not found": {http.StatusNotFound, `{"error":{"message":"gone"}}`, lib.ErrNotFound},
		"rejected":  {http.StatusBadRequest, `{"message":"bad helper","stack":"secret stack"}`, lib.ErrRejected},
		"conflict":  {http.StatusBadRequest, `{"error":{"message":"Entity with name \"x\" already exists"}}`, lib.ErrConflict},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			client, _ := newRecordingJSReport(t, map[string]reply{"GET /odata/templates('t1')": {tc.status, tc.body}})

			_, err := client.GetTemplateSource("t1", "Bearer caller")

			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Status != tc.status || apiErr.Message == "" {
				t.Errorf("err = %v, want status and message kept", err)
			}
			if strings.Contains(err.Error(), "secret stack") {
				t.Error("the stack of jsreport must not reach the error")
			}
		})
	}
}

func TestWriteUnauthorizedIsReportedAsSuch(t *testing.T) {
	client := stubJSReport(t, http.StatusUnauthorized, "")

	_, err := client.GetTemplateSource("t1", "Bearer caller")

	if !errors.Is(err, lib.ErrUnauthorized) {
		t.Fatalf("err = %v, want unauthorized", err)
	}
}
