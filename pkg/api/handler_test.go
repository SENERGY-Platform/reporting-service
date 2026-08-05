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
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/SENERGY-Platform/reporting-service/lib"
	"github.com/SENERGY-Platform/reporting-service/pkg/config"
	"github.com/SENERGY-Platform/reporting-service/pkg/report_engine"
	"github.com/gin-gonic/gin"
)

// apiTestDatabase is separate from the database the service uses, so running the
// tests can never touch real reports.
const apiTestDatabase = "reporting_test_api"

var (
	apiDBOnce sync.Once
	apiDBErr  error
)

func requireMongo(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping test that needs mongodb")
	}
	url := os.Getenv("MONGO_TEST_URL")
	if url == "" {
		url = "mongodb://localhost:27017"
	}
	apiDBOnce.Do(func() { apiDBErr = report_engine.InitDB(url, apiTestDatabase) })
	if apiDBErr != nil {
		if os.Getenv("REQUIRE_MONGO") != "" {
			t.Fatalf("no mongodb at %s: %v", url, apiDBErr)
		}
		t.Skipf("no mongodb at %s: %v", url, apiDBErr)
	}
}

// unauthorizedDriver stands in for a jsreport that rejected the token, which is
// what a missing audience in the token looks like from here.
type unauthorizedDriver struct{ stubDriver }

func (unauthorizedDriver) GetTemplates(string) ([]lib.Template, error) {
	return nil, fmt.Errorf("jsreport-unauthorized: %w", lib.ErrUnauthorized)
}

// brokenDriver fails for a reason that is not about authorization.
type brokenDriver struct{ stubDriver }

func (brokenDriver) GetTemplates(string) ([]lib.Template, error) {
	return nil, errors.New("connection refused")
}

// stubDriver stands in for jsreport. The endpoints under test only queue work, so
// nothing here is ever asked to render.
type stubDriver struct{}

func (stubDriver) GetTemplates(string) ([]lib.Template, error) { return nil, nil }
func (stubDriver) GetTemplateById(string, string) (lib.Template, error) {
	return lib.Template{}, nil
}
func (stubDriver) CreateReport(string, string, map[string]interface{}, string) (string, string, string, error) {
	return "file-1", "application/pdf", "link", nil
}
func (stubDriver) GetReportContent(string, string) ([]byte, string, string, error) {
	return nil, "", "", nil
}
func (stubDriver) DeleteCreatedReportFile(string, string) error { return nil }
func (stubDriver) GetTemplatePreview(string, string) ([]byte, string, string, error) {
	return nil, "", "", nil
}

func testServer(t *testing.T) *gin.Engine {
	t.Helper()
	return testServerWithDriver(t, stubDriver{})
}

func testServerWithDriver(t *testing.T, driver report_engine.ReportingDriver) *gin.Engine {
	t.Helper()
	requireMongo(t)
	client := report_engine.NewClient(driver, &config.Config{
		SchedulerTickerDuration: "1m",
		ReportJobWorkers:        1,
		ReportJobStaleAfter:     "2m",
		ReportJobRetention:      "168h",
	})
	engine, err := CreateServer(&config.Config{ServerPort: 8080}, client)
	if err != nil {
		t.Fatalf("could not create server: %v", err)
	}
	return engine
}

func bearer(userId string) string {
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return "Bearer " + enc(map[string]string{"alg": "none", "typ": "JWT"}) + "." +
		enc(map[string]any{"sub": userId}) + ".signature"
}

func do(t *testing.T, engine *gin.Engine, method string, path string, body string, userId string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if userId != "" {
		req.Header.Set("Authorization", bearer(userId))
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not json: %v, body %q", err, rec.Body.String())
	}
	return body
}

// TestPostReportCreateAcceptsWithoutRendering is the contract the web ui depends on:
// the call returns immediately, with the report id it can navigate to and the job
// id it can poll.
func TestPostReportCreateAcceptsWithoutRendering(t *testing.T) {
	engine := testServer(t)

	rec := do(t, engine, http.MethodPost, "/report/create", `{"name":"Verbrauch","templateName":"consumption"}`, "user-1")

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body %q", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	body := decode(t, rec)
	reportId, _ := body["id"].(string)
	if reportId == "" {
		t.Error("no report id in the response, the ui navigates with it")
	}
	jobId, _ := body["jobId"].(string)
	if jobId == "" {
		t.Error("no job id in the response, the ui polls with it")
	}
}

func TestPostReportCreateRejectsABrokenBody(t *testing.T) {
	engine := testServer(t)

	rec := do(t, engine, http.MethodPost, "/report/create", `{"name":`, "user-1")

	if rec.Code == http.StatusAccepted {
		t.Errorf("status = %d, want a failure for an unparsable body", rec.Code)
	}
}

func TestGetReportJobReportsTheQueuedStatus(t *testing.T) {
	engine := testServer(t)

	created := decode(t, do(t, engine, http.MethodPost, "/report/create", `{"name":"a"}`, "user-1"))
	jobId, _ := created["jobId"].(string)

	rec := do(t, engine, http.MethodGet, "/report/job/"+jobId, "", "user-1")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	data, ok := decode(t, rec)["data"].(map[string]any)
	if !ok {
		t.Fatalf("response has no data object: %q", rec.Body.String())
	}
	if data["status"] != string(lib.ReportJobPending) {
		t.Errorf("status = %v, want %q", data["status"], lib.ReportJobPending)
	}
	if data["id"] != jobId {
		t.Errorf("id = %v, want %q", data["id"], jobId)
	}
	// Internal bookkeeping must not reach the client.
	for _, field := range []string{"userId", "request", "sendEmail", "heartbeat"} {
		if _, present := data[field]; present {
			t.Errorf("field %q is exposed in the api response", field)
		}
	}
}

func TestGetReportJobIsNotFoundForAnotherUser(t *testing.T) {
	engine := testServer(t)

	created := decode(t, do(t, engine, http.MethodPost, "/report/create", `{"name":"geheim"}`, "owner"))
	jobId, _ := created["jobId"].(string)

	rec := do(t, engine, http.MethodGet, "/report/job/"+jobId, "", "someone-else")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestGetReportJobIsNotFoundForAnUnknownId(t *testing.T) {
	engine := testServer(t)

	rec := do(t, engine, http.MethodGet, "/report/job/does-not-exist", "", "user-1")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestGetReportJobsListsOnlyTheOwnJobs(t *testing.T) {
	engine := testServer(t)

	created := decode(t, do(t, engine, http.MethodPost, "/report/create", `{"name":"mine"}`, "user-a"))
	reportId, _ := created["id"].(string)
	do(t, engine, http.MethodPost, "/report/create", `{"name":"theirs"}`, "user-b")

	rec := do(t, engine, http.MethodGet, "/report/job?reportId="+reportId, "", "user-a")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body %q", rec.Code, http.StatusOK, rec.Body.String())
	}
	jobs, ok := decode(t, rec)["data"].([]any)
	if !ok {
		t.Fatalf("response has no data array: %q", rec.Body.String())
	}
	if len(jobs) != 1 {
		t.Fatalf("got %d jobs, want 1", len(jobs))
	}

	rec = do(t, engine, http.MethodGet, "/report/job?reportId="+reportId, "", "user-b")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	// An empty list rather than null, so a polling client can iterate it blindly.
	other, ok := decode(t, rec)["data"].([]any)
	if !ok {
		t.Fatalf("data is not an array: %q", rec.Body.String())
	}
	if len(other) != 0 {
		t.Errorf("another user sees %v, want an empty list", other)
	}
}

func TestGetReportJobsRejectsABadLimit(t *testing.T) {
	engine := testServer(t)

	for _, limit := range []string{"0", "-1", "many"} {
		rec := do(t, engine, http.MethodGet, "/report/job?limit="+limit, "", "user-1")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("limit=%s gave status %d, want %d", limit, rec.Code, http.StatusBadRequest)
		}
	}
}

// A token the report engine rejects is a configuration problem a client can act
// on, so it must be distinguishable from a generic failure.
func TestRejectedTokenIsAnsweredWithUnauthorized(t *testing.T) {
	engine := testServerWithDriver(t, unauthorizedDriver{})

	rec := do(t, engine, http.MethodGet, "/templates", "", "user-1")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body %q", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if decode(t, rec)["error"] != MessageUnauthorized {
		t.Errorf("error = %v, want %q", decode(t, rec)["error"], MessageUnauthorized)
	}
}

func TestOtherFailuresAreNotAnsweredWithUnauthorized(t *testing.T) {
	engine := testServerWithDriver(t, brokenDriver{})

	rec := do(t, engine, http.MethodGet, "/templates", "", "user-1")

	if rec.Code == http.StatusUnauthorized {
		t.Errorf("status = %d, want anything but %d for a non-auth failure", rec.Code, http.StatusUnauthorized)
	}
	if rec.Code == http.StatusOK {
		t.Errorf("status = %d, want a failure", rec.Code)
	}
}

func TestReportEndpointsRequireIdentity(t *testing.T) {
	engine := testServer(t)

	for _, path := range []string{"/report/job", "/report/job/some-id"} {
		rec := do(t, engine, http.MethodGet, path, "", "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without identity gave %d, want %d", path, rec.Code, http.StatusUnauthorized)
		}
	}
	rec := do(t, engine, http.MethodPost, "/report/create", `{"name":"a"}`, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST /report/create without identity gave %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
