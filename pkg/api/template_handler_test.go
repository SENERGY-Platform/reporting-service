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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SENERGY-Platform/reporting-service/lib"
	"github.com/SENERGY-Platform/reporting-service/pkg/apis/device_repository"
	"github.com/SENERGY-Platform/reporting-service/pkg/apis/figures"
	"github.com/SENERGY-Platform/reporting-service/pkg/apis/jsreport"
	"github.com/SENERGY-Platform/reporting-service/pkg/config"
	"github.com/SENERGY-Platform/reporting-service/pkg/report_engine"
	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
)

// authoringDriver records what the template endpoints ask of the report engine.
type authoringDriver struct {
	stubDriver
	folder      string
	parent      string
	group       string
	subErr      error
	createdIn   string
	templateIn  map[string]string
	created     []lib.TemplateInput
	folderErr   error
	createErr   error
	deleted     []string
	changeErr   error
	sourceErr   error
	updatedIn   string
	rendered    []lib.PreviewRender
	renderData  map[string]interface{}
	source      lib.TemplateSource
	contentType string
}

func (d *authoringDriver) FindFolderShortId(name string, _ string) (string, error) {
	d.folder = name
	return "fshort", d.folderErr
}
func (d *authoringDriver) FindSubfolderShortId(parent string, name string, _ string) (string, error) {
	d.parent = parent
	d.group = name
	return "sub-" + name, d.subErr
}
func (d *authoringDriver) CreateTemplate(folder string, input lib.TemplateInput, _ string) (lib.Template, error) {
	if d.createErr != nil {
		return lib.Template{}, d.createErr
	}
	d.createdIn = folder
	d.created = append(d.created, input)
	return lib.Template{Id: "t1", Name: input.Name, Type: "PDF"}, nil
}
func (d *authoringDriver) UpdateTemplate(folder string, id string, input lib.TemplateInput, _ string) (lib.Template, error) {
	d.updatedIn = folder
	if in, ok := d.templateIn[id]; ok && in != folder {
		return lib.Template{}, fmt.Errorf("%w: template %s", lib.ErrOutsideTemplateFolder, id)
	}
	if d.changeErr != nil {
		return lib.Template{}, d.changeErr
	}
	return lib.Template{Id: id, Name: input.Name}, nil
}
func (d *authoringDriver) DeleteTemplate(folder string, id string, _ string) error {
	if in, ok := d.templateIn[id]; ok && in != folder {
		return fmt.Errorf("%w: template %s", lib.ErrOutsideTemplateFolder, id)
	}
	if d.changeErr != nil {
		return d.changeErr
	}
	d.deleted = append(d.deleted, id)
	return nil
}
func (d *authoringDriver) GetTemplateSource(string, string) (lib.TemplateSource, error) {
	return d.source, d.sourceErr
}

func (d *authoringDriver) GetTemplateById(string, string) (lib.Template, error) {
	return lib.Template{}, d.sourceErr
}
func (d *authoringDriver) RenderInline(render lib.PreviewRender, data map[string]interface{}, _ string) ([]byte, string, error) {
	d.rendered = append(d.rendered, render)
	d.renderData = data
	return []byte("PNG"), d.contentType, nil
}

const testAuthorRole = "template-author"

// authoringServer needs no database: the template endpoints never touch it.
func authoringServer(t *testing.T, driver report_engine.ReportingDriver, templates config.TemplateConfig) *gin.Engine {
	t.Helper()
	client := &report_engine.Client{Driver: driver, Config: &config.Config{Templates: templates}}
	engine, err := CreateServer(&config.Config{ServerPort: 8080}, client)
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func configured() config.TemplateConfig {
	return config.TemplateConfig{Folder: "custom", AuthorRole: testAuthorRole, GroupClaim: "group"}
}

// bearerWithRoles carries the group claim "g1", so the caller has a report group.
func bearerWithRoles(userId string, roles ...string) string {
	return bearerWithClaims(map[string]any{"sub": userId, "group": "g1", "realm_access": map[string]any{"roles": roles}})
}

func bearerWithClaims(claims map[string]any) string {
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return "Bearer " + enc(map[string]string{"alg": "none", "typ": "JWT"}) + "." +
		enc(claims) + ".signature"
}

func send(t *testing.T, engine *gin.Engine, method string, path string, body string, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

const validTemplate = `{"name":"Verbrauch","recipe":"chrome-pdf","content":"<h1>{{title}}</h1>","sampleData":{"title":"x"}}`

func TestTemplateWritesAreNotFoundWithoutConfiguration(t *testing.T) {
	for name, cfg := range map[string]config.TemplateConfig{
		"nothing":   {},
		"no role":   {Folder: "custom"},
		"no folder": {AuthorRole: testAuthorRole},
	} {
		t.Run(name, func(t *testing.T) {
			driver := &authoringDriver{}
			engine := authoringServer(t, driver, cfg)
			token := bearerWithRoles("u1", testAuthorRole)
			for _, call := range []struct{ method, path string }{
				{http.MethodPost, "/templates"},
				{http.MethodPut, "/templates/t1"},
				{http.MethodDelete, "/templates/t1"},
				{http.MethodPost, "/templates/preview"},
			} {
				rec := send(t, engine, call.method, call.path, validTemplate, token)
				if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), MessageTemplatesNotConfigured) {
					t.Errorf("%s %s = %d %q, want 404 with the reason", call.method, call.path, rec.Code, rec.Body.String())
				}
			}
			if len(driver.created) != 0 || driver.folder != "" {
				t.Error("an unconfigured service must not reach the report engine")
			}
		})
	}
}

func TestTemplateWritesNeedTheAuthorRole(t *testing.T) {
	driver := &authoringDriver{}
	engine := authoringServer(t, driver, configured())
	for name, token := range map[string]string{
		"no roles":   bearerWithRoles("u1"),
		"other role": bearerWithRoles("u1", "admin", "user"),
	} {
		t.Run(name, func(t *testing.T) {
			for _, call := range []struct{ method, path string }{
				{http.MethodPost, "/templates"},
				{http.MethodPut, "/templates/t1"},
				{http.MethodDelete, "/templates/t1"},
				{http.MethodPost, "/templates/preview"},
			} {
				rec := send(t, engine, call.method, call.path, validTemplate, token)
				if rec.Code != http.StatusForbidden {
					t.Errorf("%s %s = %d, want 403", call.method, call.path, rec.Code)
				}
			}
		})
	}
	if len(driver.created) != 0 || len(driver.deleted) != 0 || driver.folder != "" {
		t.Error("a caller without the role must not reach the report engine")
	}
}

func TestListingTemplatesDoesNotNeedTheRole(t *testing.T) {
	engine := authoringServer(t, &authoringDriver{}, configured())

	rec := send(t, engine, http.MethodGet, "/templates", "", bearerWithRoles("u1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /templates = %d, want 200", rec.Code)
	}
}

func TestPostTemplateCreatesInTheCallersGroupFolder(t *testing.T) {
	driver := &authoringDriver{}
	engine := authoringServer(t, driver, configured())

	rec := send(t, engine, http.MethodPost, "/templates", validTemplate, bearerWithRoles("u1", testAuthorRole))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	if driver.folder != "custom" || len(driver.created) != 1 || driver.created[0].Name != "Verbrauch" {
		t.Errorf("folder %q, created %+v", driver.folder, driver.created)
	}
	if driver.parent != "fshort" || driver.group != "g1" || driver.createdIn != "sub-g1" {
		t.Errorf("subfolder %q below %q, created in %q, want g1 below the configured folder", driver.group, driver.parent, driver.createdIn)
	}
	data, _ := decode(t, rec)["data"].(map[string]any)
	if data["id"] != "t1" {
		t.Errorf("answer = %v, want the template", data)
	}
}

func TestPostTemplateRefusesInvalidInput(t *testing.T) {
	long := strings.Repeat("a", maxTemplateName+1)
	cases := map[string]string{
		"empty name":         `{"name":"  ","recipe":"chrome-pdf","content":"x","sampleData":{}}`,
		"name too long":      fmt.Sprintf(`{"name":%q,"recipe":"chrome-pdf","content":"x","sampleData":{}}`, long),
		"name with slash":    `{"name":"a/b","recipe":"chrome-pdf","content":"x","sampleData":{}}`,
		"unknown recipe":     `{"name":"n","recipe":"html","content":"x","sampleData":{}}`,
		"image recipe":       `{"name":"n","recipe":"chrome-image","content":"x","sampleData":{}}`,
		"empty content":      `{"name":"n","recipe":"chrome-pdf","content":"","sampleData":{}}`,
		"content too large":  fmt.Sprintf(`{"name":"n","recipe":"chrome-pdf","content":%q,"sampleData":{}}`, strings.Repeat("x", maxTemplateContent+1)),
		"helpers too large":  fmt.Sprintf(`{"name":"n","recipe":"chrome-pdf","content":"x","helpers":%q,"sampleData":{}}`, strings.Repeat("x", maxTemplateHelpers+1)),
		"helper override":    `{"name":"n","recipe":"chrome-pdf","content":"x","helpers":"function number(v) { return v }","sampleData":{}}`,
		"sample data large":  fmt.Sprintf(`{"name":"n","recipe":"chrome-pdf","content":"x","sampleData":{"a":%q}}`, strings.Repeat("x", maxSampleData+1)),
		"sample data array":  `{"name":"n","recipe":"chrome-pdf","content":"x","sampleData":[1]}`,
		"sample data string": `{"name":"n","recipe":"chrome-pdf","content":"x","sampleData":"{}"}`,
		"engine given":       `{"name":"n","recipe":"chrome-pdf","engine":"jsrender","content":"x","sampleData":{}}`,
		"scripts given":      `{"name":"n","recipe":"chrome-pdf","content":"x","scripts":[{"shortid":"s"}],"sampleData":{}}`,
		"not json":           `{"name":`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			driver := &authoringDriver{}
			engine := authoringServer(t, driver, configured())

			rec := send(t, engine, http.MethodPost, "/templates", body, bearerWithRoles("u1", testAuthorRole))

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400, body %q", rec.Code, rec.Body.String())
			}
			if len(driver.created) != 0 {
				t.Error("an invalid template reached the report engine")
			}
		})
	}
}

func TestPostTemplateWithAFolderTheCallerCannotReadIsNotAServerError(t *testing.T) {
	driver := &authoringDriver{folderErr: fmt.Errorf("%w: %q", lib.ErrTemplateFolderAccess, "custom")}
	engine := authoringServer(t, driver, configured())

	rec := send(t, engine, http.MethodPost, "/templates", validTemplate, bearerWithRoles("u1", testAuthorRole))

	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), MessageTemplateFolder) {
		t.Errorf("status = %d body %q, want 403 template folder not readable", rec.Code, rec.Body.String())
	}
}

func TestPostTemplateWithoutAGroupClaimIsRefused(t *testing.T) {
	roles := map[string]any{"roles": []string{testAuthorRole}}
	for name, claims := range map[string]map[string]any{
		"claim missing": {"sub": "u1", "realm_access": roles},
		"claim empty":   {"sub": "u1", "group": "  ", "realm_access": roles},
		"not a string":  {"sub": "u1", "group": []string{"a"}, "realm_access": roles},
		"other claim":   {"sub": "u1", "org": "g1", "realm_access": roles},
	} {
		t.Run(name, func(t *testing.T) {
			driver := &authoringDriver{}
			engine := authoringServer(t, driver, configured())

			rec := send(t, engine, http.MethodPost, "/templates", validTemplate, bearerWithClaims(claims))

			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), MessageNoReportGroup) {
				t.Errorf("status = %d body %q, want 403 %s", rec.Code, rec.Body.String(), MessageNoReportGroup)
			}
			if len(driver.created) != 0 || driver.group != "" {
				t.Errorf("created %+v after looking up %q, want no lookup", driver.created, driver.group)
			}
		})
	}
}

func TestPostTemplateReadsTheGroupFromTheConfiguredClaim(t *testing.T) {
	driver := &authoringDriver{}
	cfg := configured()
	cfg.GroupClaim = "org"
	engine := authoringServer(t, driver, cfg)
	token := bearerWithClaims(map[string]any{"sub": "u1", "group": "g1", "org": "acme", "realm_access": map[string]any{"roles": []string{testAuthorRole}}})

	rec := send(t, engine, http.MethodPost, "/templates", validTemplate, token)

	if rec.Code != http.StatusCreated || driver.createdIn != "sub-acme" {
		t.Errorf("status = %d, created in %q, want sub-acme", rec.Code, driver.createdIn)
	}
}

func TestPostTemplateWithoutASubfolderForTheGroupIsNotReadable(t *testing.T) {
	driver := &authoringDriver{subErr: fmt.Errorf("%w: %q", lib.ErrTemplateFolderAccess, "g1")}
	engine := authoringServer(t, driver, configured())

	rec := send(t, engine, http.MethodPost, "/templates", validTemplate, bearerWithRoles("u1", testAuthorRole))

	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), MessageTemplateFolder) {
		t.Errorf("status = %d body %q, want 403 template folder not readable", rec.Code, rec.Body.String())
	}
}

func TestChangingATemplateOfAnotherGroupsFolderIsRefused(t *testing.T) {
	driver := &authoringDriver{templateIn: map[string]string{"t9": "sub-other"}}
	engine := authoringServer(t, driver, configured())
	token := bearerWithRoles("u1", testAuthorRole)

	for method, body := range map[string]string{http.MethodPut: validTemplate, http.MethodDelete: ""} {
		rec := send(t, engine, method, "/templates/t9", body, token)

		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), MessageOutsideFolder) {
			t.Errorf("%s status = %d body %q, want 403 %s", method, rec.Code, rec.Body.String(), MessageOutsideFolder)
		}
	}
	if len(driver.deleted) != 0 {
		t.Errorf("deleted = %v, want nothing", driver.deleted)
	}
}

func TestPostTemplateTranslatesEngineErrors(t *testing.T) {
	cases := map[string]struct {
		err    error
		status int
		body   string
	}{
		"duplicate as 500": {&jsreport.APIError{Status: 500, Message: `Entity with name "Verbrauch" already exists in the /custom folder.`}, http.StatusConflict, "already exists"},
		"duplicate":        {&jsreport.APIError{Status: 400, Message: `Entity with name "Verbrauch" already exists`}, http.StatusConflict, "already exists"},
		"forbidden":        {&jsreport.APIError{Status: 403, Message: "Permission denied"}, http.StatusForbidden, MessageNoFolderWrite},
		"rejected":         {&jsreport.APIError{Status: 400, Message: "invalid helper"}, http.StatusBadRequest, "invalid helper"},
		"token":            {jsreport.ErrUnauthorized, http.StatusUnauthorized, MessageUnauthorized},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			engine := authoringServer(t, &authoringDriver{createErr: tc.err}, configured())

			rec := send(t, engine, http.MethodPost, "/templates", validTemplate, bearerWithRoles("u1", testAuthorRole))

			if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.body) {
				t.Errorf("status = %d body %q, want %d containing %q", rec.Code, rec.Body.String(), tc.status, tc.body)
			}
		})
	}
}

func TestPutAndDeleteTemplate(t *testing.T) {
	driver := &authoringDriver{}
	engine := authoringServer(t, driver, configured())
	token := bearerWithRoles("u1", testAuthorRole)

	put := send(t, engine, http.MethodPut, "/templates/t7", validTemplate, token)
	del := send(t, engine, http.MethodDelete, "/templates/t7", "", token)

	if put.Code != http.StatusOK || del.Code != http.StatusNoContent {
		t.Fatalf("put = %d, delete = %d", put.Code, del.Code)
	}
	if id, _ := decode(t, put)["data"].(map[string]any)["id"].(string); id != "t7" {
		t.Errorf("put answered template %q, want t7", id)
	}
	if len(driver.deleted) != 1 || driver.deleted[0] != "t7" {
		t.Errorf("deleted = %v", driver.deleted)
	}
}

func TestPreviewRendersAnImageByDefaultAndTheFileWithAsFile(t *testing.T) {
	body := `{"template":{"recipe":"html-to-xlsx","content":"<table>{{title}}</table>"},"data":{"title":{"name":"title","valueType":"string","value":"Hallo"}}}`
	token := bearerWithRoles("u1", testAuthorRole)

	for name, tc := range map[string]struct {
		path  string
		image bool
	}{"image": {"/templates/preview", true}, "file": {"/templates/preview?as=file", false}} {
		t.Run(name, func(t *testing.T) {
			driver := &authoringDriver{contentType: "image/png"}
			engine := authoringServer(t, driver, configured())

			rec := send(t, engine, http.MethodPost, tc.path, body, token)

			if rec.Code != http.StatusOK || rec.Body.String() != "PNG" {
				t.Fatalf("status = %d body %q", rec.Code, rec.Body.String())
			}
			if len(driver.rendered) != 1 || driver.rendered[0].Image != tc.image || driver.rendered[0].Recipe != "html-to-xlsx" {
				t.Errorf("rendered = %+v, want image=%v with the draft's recipe", driver.rendered, tc.image)
			}
			if driver.renderData["title"] != "Hallo" {
				t.Errorf("data = %v, want it filled like a run", driver.renderData)
			}
			if len(driver.created) != 0 {
				t.Error("a preview must not store anything")
			}
		})
	}
}

func TestPreviewOfAStoredTemplateLoadsItsSource(t *testing.T) {
	driver := &authoringDriver{source: lib.TemplateSource{Recipe: "chrome-pdf", Content: "stored", Helpers: "h"}}
	engine := authoringServer(t, driver, configured())

	rec := send(t, engine, http.MethodPost, "/templates/preview", `{"templateId":"t1","data":{}}`, bearerWithRoles("u1", testAuthorRole))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %q", rec.Code, rec.Body.String())
	}
	if len(driver.rendered) != 1 || driver.rendered[0].Content != "stored" || driver.rendered[0].Helpers != "h" {
		t.Errorf("rendered = %+v", driver.rendered)
	}
}

func TestPreviewRefusesInvalidInput(t *testing.T) {
	cases := map[string]struct{ path, body string }{
		"neither":         {"/templates/preview", `{"data":{}}`},
		"both":            {"/templates/preview", `{"templateId":"t1","template":{"recipe":"chrome-pdf","content":"x"}}`},
		"bad recipe":      {"/templates/preview", `{"template":{"recipe":"html","content":"x"}}`},
		"empty content":   {"/templates/preview", `{"template":{"recipe":"chrome-pdf","content":""}}`},
		"unknown as":      {"/templates/preview?as=pdf", `{"templateId":"t1"}`},
		"bad interval":    {"/templates/preview", `{"template":{"recipe":"chrome-pdf","content":"x"},"data":{"c":{"name":"c","valueType":"object","figuresQuery":{"graphId":"g","interval":"hour","period":{"kind":"previousMonth"}}}}}`},
		"helper override": {"/templates/preview", `{"template":{"recipe":"chrome-pdf","content":"x","helpers":"function date(v) {}"}}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			driver := &authoringDriver{}
			engine := authoringServer(t, driver, configured())

			rec := send(t, engine, http.MethodPost, tc.path, tc.body, bearerWithRoles("u1", testAuthorRole))

			if rec.Code != http.StatusBadRequest || len(driver.rendered) != 0 {
				t.Errorf("status = %d, rendered %d, want 400 and nothing rendered", rec.Code, len(driver.rendered))
			}
		})
	}
}

func TestPreviewDataFailureIsAnAuthorFacingError(t *testing.T) {
	driver := &authoringDriver{}
	engine := authoringServer(t, driver, configured())
	body := `{"template":{"recipe":"chrome-pdf","content":"x"},"data":{"consumption":{"name":"consumption","valueType":"object","figuresQuery":{"graphId":"g","period":{"kind":"previousMonth"}}}}}`

	rec := send(t, engine, http.MethodPost, "/templates/preview", body, bearerWithRoles("u1", testAuthorRole))

	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "consumption") {
		t.Errorf("status = %d body %q, want 422 naming the report object", rec.Code, rec.Body.String())
	}
	if len(driver.rendered) != 0 {
		t.Error("nothing may be rendered when the data could not be collected")
	}
}

// The three reasons for a 403 have to read differently, so the caller knows what to ask for.
func TestForbiddenAnswersNameTheirReason(t *testing.T) {
	denied := &jsreport.APIError{Status: 403, Message: "Permission denied"}
	cases := map[string]struct {
		method, path string
		driver       *authoringDriver
		token        string
		want         string
	}{
		"role missing":          {http.MethodPost, "/templates", &authoringDriver{}, bearerWithRoles("u1"), MessageRoleMissing},
		"folder not readable":   {http.MethodPost, "/templates", &authoringDriver{folderErr: fmt.Errorf("%w", lib.ErrTemplateFolderAccess)}, bearerWithRoles("u1", testAuthorRole), MessageTemplateFolder},
		"no write on folder":    {http.MethodPost, "/templates", &authoringDriver{createErr: denied}, bearerWithRoles("u1", testAuthorRole), MessageNoFolderWrite},
		"no write on template":  {http.MethodPut, "/templates/t1", &authoringDriver{changeErr: denied}, bearerWithRoles("u1", testAuthorRole), MessageNoTemplateWrite},
		"no delete on template": {http.MethodDelete, "/templates/t1", &authoringDriver{changeErr: denied}, bearerWithRoles("u1", testAuthorRole), MessageNoTemplateWrite},
		"outside the folder":    {http.MethodPut, "/templates/t1", &authoringDriver{changeErr: fmt.Errorf("%w: template t1", lib.ErrOutsideTemplateFolder)}, bearerWithRoles("u1", testAuthorRole), MessageOutsideFolder},
		"delete outside":        {http.MethodDelete, "/templates/t1", &authoringDriver{changeErr: fmt.Errorf("%w: template t1", lib.ErrOutsideTemplateFolder)}, bearerWithRoles("u1", testAuthorRole), MessageOutsideFolder},
	}
	seen := map[string]string{}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			engine := authoringServer(t, tc.driver, configured())

			rec := send(t, engine, tc.method, tc.path, validTemplate, tc.token)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d body %q, want 403", rec.Code, rec.Body.String())
			}
			message, _ := decode(t, rec)["error"].(string)
			if message != tc.want {
				t.Errorf("message = %q, want %q", message, tc.want)
			}
			if other, dup := seen[message]; dup && message != MessageOutsideFolder && message != MessageNoTemplateWrite {
				t.Errorf("message %q is also the answer for %s", message, other)
			}
			seen[message] = name
		})
	}
}

func TestUnknownTemplateIsNotFound(t *testing.T) {
	missing := fmt.Errorf("%w: template nope", lib.ErrNotFound)
	driver := &authoringDriver{changeErr: missing, sourceErr: missing}
	engine := authoringServer(t, driver, configured())
	token := bearerWithRoles("u1", testAuthorRole)

	for _, call := range []struct{ method, path, body string }{
		{http.MethodPut, "/templates/nope", validTemplate},
		{http.MethodDelete, "/templates/nope", ""},
		{http.MethodPost, "/templates/preview", `{"templateId":"nope"}`},
		{http.MethodGet, "/templates/nope", ""},
	} {
		rec := send(t, engine, call.method, call.path, call.body, token)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), MessageTemplateNotFound) {
			t.Errorf("%s %s = %d %q, want 404", call.method, call.path, rec.Code, rec.Body.String())
		}
	}
}

// figures' 400 for too many steps is the author's to fix, so it is no 422 or 500.
func TestPreviewPassesFiguresSentenceForTooManyStepsOnAsA400(t *testing.T) {
	sentence := "the range has 731 day steps, at most 400 are allowed: use week or a shorter range"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/consumption/totals") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"` + sentence + `"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"g","nodes":[],"edges":[]}`))
	}))
	defer srv.Close()
	driver := &authoringDriver{}
	client := &report_engine.Client{
		Driver:           driver,
		Config:           &config.Config{Templates: configured()},
		Figures:          &figures.Client{BaseUrl: srv.URL + "/figures", HttpClient: resty.New()},
		DeviceRepository: &device_repository.Client{BaseUrl: srv.URL, HttpClient: resty.New()},
	}
	engine, err := CreateServer(&config.Config{ServerPort: 8080}, client)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"template":{"recipe":"chrome-pdf","content":"x"},"data":{"consumption":{"name":"consumption","valueType":"object","figuresQuery":{"graphId":"g","interval":"day","period":{"kind":"fixed","start":"2025-01-01","end":"2026-12-31"}}}}}`

	rec := send(t, engine, http.MethodPost, "/templates/preview", body, bearerWithRoles("u1", testAuthorRole))

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), sentence) {
		t.Errorf("status = %d body %q, want 400 with figures' sentence", rec.Code, rec.Body.String())
	}
	if len(driver.rendered) != 0 {
		t.Error("nothing may be rendered when figures refused the query")
	}
}
