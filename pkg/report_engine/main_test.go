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
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/SENERGY-Platform/reporting-service/lib"
	"github.com/SENERGY-Platform/reporting-service/pkg/config"
	"github.com/SENERGY-Platform/reporting-service/pkg/util"
	"go.mongodb.org/mongo-driver/bson"
)

// testDatabase keeps the integration tests away from the database the service
// uses, so running them can never delete real reports.
const testDatabase = "reporting_test"

func TestMain(m *testing.M) {
	util.InitStructLogger("error")
	code := m.Run()
	if DB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), dbOpTimeout)
		if err := DB.Database(testDatabase).Drop(ctx); err != nil {
			_, _ = os.Stderr.WriteString("could not drop test database: " + err.Error() + "\n")
		}
		cancel()
	}
	os.Exit(code)
}

func mongoURL() string {
	if url := os.Getenv("MONGO_TEST_URL"); url != "" {
		return url
	}
	return "mongodb://localhost:27017"
}

var (
	initDBOnce sync.Once
	initDBErr  error
)

// requireMongo connects to a real mongodb and empties the test collections. It
// skips when no mongodb is reachable, unless REQUIRE_MONGO is set, which is how CI
// makes sure these tests are not quietly skipped there.
func requireMongo(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping test that needs mongodb")
	}
	initDBOnce.Do(func() { initDBErr = InitDB(mongoURL(), testDatabase) })
	if initDBErr != nil {
		if os.Getenv("REQUIRE_MONGO") != "" {
			t.Fatalf("no mongodb at %s: %v", mongoURL(), initDBErr)
		}
		t.Skipf("no mongodb at %s: %v", mongoURL(), initDBErr)
	}
	ctx, cancel := dbCtx()
	defer cancel()
	for _, col := range []string{"reports", "report_jobs"} {
		if _, err := DB.Database(testDatabase).Collection(col).DeleteMany(ctx, bson.M{}); err != nil {
			t.Fatalf("could not clear %s: %v", col, err)
		}
	}
}

// testToken builds an unsigned token for the given user. jwt.Parse does not verify
// signatures, so this is enough to exercise everything that reads the user id.
func testToken(t *testing.T, userId string) string {
	t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("could not encode token part: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	header := enc(map[string]string{"alg": "none", "typ": "JWT"})
	payload := enc(map[string]any{"sub": userId, "preferred_username": userId})
	return "Bearer " + header + "." + payload + ".signature"
}

// keycloakStub answers the token exchange call of a report job worker with a token
// for the requested subject.
func keycloakStub(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		userId := r.PostForm.Get("requested_subject")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": testToken(t, userId)[len("Bearer "):],
			"expires_in":   300,
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fakeDriver is a ReportingDriver that records what it was asked to render instead
// of talking to jsreport.
type fakeDriver struct {
	mu        sync.Mutex
	rendered  []fakeRender
	renderErr error
	// renderDelay simulates a report that takes a while to build.
	renderDelay time.Duration
	deleted     []string
}

type fakeRender struct {
	FileId       string
	ReportName   string
	TemplateName string
	Data         map[string]interface{}
}

func (d *fakeDriver) CreateReport(reportName string, templateName string, data map[string]interface{}, _ string) (string, string, string, error) {
	if d.renderDelay > 0 {
		time.Sleep(d.renderDelay)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.renderErr != nil {
		return "", "", "", d.renderErr
	}
	render := fakeRender{
		FileId:       "file-" + reportName + "-" + time.Now().Format("150405.000000000"),
		ReportName:   reportName,
		TemplateName: templateName,
		Data:         data,
	}
	d.rendered = append(d.rendered, render)
	return render.FileId, "application/pdf", "link/" + render.FileId, nil
}

func (d *fakeDriver) renders() []fakeRender {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]fakeRender(nil), d.rendered...)
}

func (d *fakeDriver) GetTemplates(string) ([]lib.Template, error) {
	return nil, nil
}

func (d *fakeDriver) GetTemplateById(string, string) (lib.Template, error) {
	return lib.Template{}, nil
}

func (d *fakeDriver) GetReportContent(reportId string, _ string) ([]byte, string, string, error) {
	return []byte("content of " + reportId), "application/pdf", "pdf", nil
}

func (d *fakeDriver) DeleteCreatedReportFile(reportId string, _ string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deleted = append(d.deleted, reportId)
	return nil
}

func (d *fakeDriver) GetTemplatePreview(id string, _ string) ([]byte, string, string, error) {
	return []byte("preview of " + id), "application/pdf", "pdf", nil
}

// testClient builds a Client whose report rendering and token exchange are stubbed.
func testClient(t *testing.T, driver *fakeDriver) *Client {
	t.Helper()
	keycloak := keycloakStub(t)
	return &Client{
		Driver: driver,
		Config: &config.Config{
			Keycloak: config.KeycloakConfig{
				Url:          keycloak.URL,
				ClientId:     "reporting-service",
				ClientSecret: "secret",
			},
			SchedulerTickerDuration: "1m",
			ReportJobWorkers:        1,
			ReportJobStaleAfter:     "2m",
			ReportJobRetention:      "168h",
		},
		jobs: newJobQueue(),
	}
}
