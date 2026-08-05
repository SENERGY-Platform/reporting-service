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
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/SENERGY-Platform/reporting-service/pkg/config"
	"github.com/SENERGY-Platform/reporting-service/pkg/report_engine"
	"github.com/SENERGY-Platform/reporting-service/pkg/util"
	"github.com/gin-gonic/gin"
)

func TestMain(m *testing.M) {
	util.InitStructLogger("error")
	gin.SetMode(gin.TestMode)
	code := m.Run()
	if report_engine.DB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := report_engine.DB.Database(apiTestDatabase).Drop(ctx); err != nil {
			_, _ = os.Stderr.WriteString("could not drop test database: " + err.Error() + "\n")
		}
		cancel()
	}
	os.Exit(code)
}

// TestCreateServerRegistersRoutes pins the route table. The job routes sit next to
// the /report/:id wildcard, which is exactly the kind of neighbourhood a router
// tree can refuse to build.
func TestCreateServerRegistersRoutes(t *testing.T) {
	engine, err := CreateServer(&config.Config{ServerPort: 8080, URLPrefix: "/reporting"}, &report_engine.Client{})
	if err != nil {
		t.Fatalf("could not create server: %v", err)
	}

	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}

	want := []string{
		http.MethodPost + " /reporting/report/create",
		http.MethodGet + " /reporting/report/job",
		http.MethodGet + " /reporting/report/job/:jobId",
		http.MethodGet + " /reporting/report/:id",
		http.MethodGet + " /reporting/report",
		http.MethodGet + " /reporting/report/file/:reportId/:fileId",
		http.MethodGet + " /reporting/health-check",
	}
	for _, route := range want {
		if !registered[route] {
			t.Errorf("route %q is not registered, got %v", route, engine.Routes())
		}
	}
}

// TestReportJobRouteWinsOverTheReportIdWildcard makes sure /report/job is not
// swallowed by /report/:id, which would break status polling.
func TestReportJobRouteWinsOverTheReportIdWildcard(t *testing.T) {
	engine := gin.New()
	engine.GET("/report/:id", func(c *gin.Context) { c.String(http.StatusOK, "report") })
	engine.GET("/report/job", func(c *gin.Context) { c.String(http.StatusOK, "job list") })
	engine.GET("/report/job/:jobId", func(c *gin.Context) { c.String(http.StatusOK, "job "+c.Param("jobId")) })

	cases := map[string]string{
		"/report/job":       "job list",
		"/report/job/abc":   "job abc",
		"/report/some-uuid": "report",
	}
	for path, want := range cases {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want %d", path, rec.Code, http.StatusOK)
			continue
		}
		if rec.Body.String() != want {
			t.Errorf("GET %s served %q, want %q", path, rec.Body.String(), want)
		}
	}
}

func TestAuthMiddlewareRejectsARequestWithoutIdentity(t *testing.T) {
	engine := gin.New()
	engine.Use(AuthMiddleware())
	engine.GET("/protected", func(c *gin.Context) { c.Status(http.StatusOK) })

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/protected", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddlewarePassesTheUserIdHeaderThrough(t *testing.T) {
	engine := gin.New()
	engine.Use(AuthMiddleware())
	var seen string
	engine.GET("/protected", func(c *gin.Context) {
		seen = c.GetString(UserIdKey)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("X-UserId", "user-1")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if seen != "user-1" {
		t.Errorf("user id = %q, want %q", seen, "user-1")
	}
}

func TestGetUserId(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		query   string
		want    string
		wantErr bool
	}{
		{
			name:    "reads the user id header",
			headers: map[string]string{"X-UserId": "user-1"},
			want:    "user-1",
		},
		{
			name:    "lets an admin ask on behalf of another user",
			headers: map[string]string{"X-UserId": "admin-1", "X-User-Roles": "developer, admin"},
			query:   "?for_user=user-2",
			want:    "user-2",
		},
		{
			name:    "ignores for_user for a non admin",
			headers: map[string]string{"X-UserId": "user-1", "X-User-Roles": "developer"},
			query:   "?for_user=user-2",
			want:    "user-1",
		},
		{
			name:    "fails without any identity",
			headers: map[string]string{},
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gc, _ := gin.CreateTestContext(httptest.NewRecorder())
			gc.Request = httptest.NewRequest(http.MethodGet, "/report"+c.query, nil)
			for k, v := range c.headers {
				gc.Request.Header.Set(k, v)
			}

			got, err := getUserId(gc)
			if c.wantErr {
				if err == nil {
					t.Fatalf("got user id %q, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("user id = %q, want %q", got, c.want)
			}
		})
	}
}
