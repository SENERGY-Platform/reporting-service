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
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SENERGY-Platform/reporting-service/lib"
	"github.com/go-resty/resty/v2"
)

// stubJSReport answers every request with the given status and body.
func stubJSReport(t *testing.T, status int, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseUrl: srv.URL, HttpClient: resty.New()}
}

// jsreport answers a request it does not accept with 401 and an empty body. That
// used to be unmarshalled, which reported a json error and hid the real cause.
func TestUnauthorizedIsReportedAsSuchNotAsAJSONError(t *testing.T) {
	cases := map[string]func(c *Client) error{
		"GetTemplates": func(c *Client) error {
			_, err := c.GetTemplates("Bearer t")
			return err
		},
		"GetTemplateById": func(c *Client) error {
			_, err := c.GetTemplateById("t1", "Bearer t")
			return err
		},
		"GetTemplatePreview": func(c *Client) error {
			_, _, _, err := c.GetTemplatePreview("t1", "Bearer t")
			return err
		},
		"GetReportContent": func(c *Client) error {
			_, _, _, err := c.GetReportContent("r1", "Bearer t")
			return err
		},
		"CreateReport": func(c *Client) error {
			_, _, _, err := c.CreateReport("report", "template", nil, "Bearer t")
			return err
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			err := call(stubJSReport(t, http.StatusUnauthorized, ""))
			if !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("err = %v, want ErrUnauthorized", err)
			}
			// the api layer recognises the shared sentinel to answer with 401
			if !errors.Is(err, lib.ErrUnauthorized) {
				t.Errorf("err = %v, want it to wrap lib.ErrUnauthorized", err)
			}
			if strings.Contains(err.Error(), "JSON") || strings.Contains(err.Error(), "json") {
				t.Errorf("err = %q, want it to not blame the json", err)
			}
		})
	}
}

func TestDeleteCreatedReportFileAcceptsAFileJSReportDoesNotKnow(t *testing.T) {
	client := stubJSReport(t, http.StatusNotFound, `{"error":{"message":"Report f1 not found"}}`)

	if err := client.DeleteCreatedReportFile("f1", "Bearer t"); err != nil {
		t.Errorf("err = %v, want nil for a file that is already gone", err)
	}
}

func TestDeleteCreatedReportFileReportsARejectedToken(t *testing.T) {
	client := stubJSReport(t, http.StatusUnauthorized, "")

	err := client.DeleteCreatedReportFile("f1", "Bearer t")

	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	// it used to build the error from an empty body, losing the message entirely
	if err.Error() == "" {
		t.Error("error has no message")
	}
}

func TestDeleteCreatedReportFileDeletesTheFile(t *testing.T) {
	client := stubJSReport(t, http.StatusNoContent, "")

	if err := client.DeleteCreatedReportFile("f1", "Bearer t"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGetTemplatesReportsTheStatusOfAFailedRequest(t *testing.T) {
	client := stubJSReport(t, http.StatusInternalServerError, "")

	_, err := client.GetTemplates("Bearer t")

	if err == nil {
		t.Fatal("got no error for a 500, want one")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %q, want it to name the status", err)
	}
}

func TestGetTemplatesPassesOnTheJSReportErrorMessage(t *testing.T) {
	client := stubJSReport(t, http.StatusBadRequest, `{"message":"bad request","error":{"message":"template name is required"}}`)

	_, err := client.GetTemplates("Bearer t")

	if err == nil {
		t.Fatal("got no error, want one")
	}
	if !strings.Contains(err.Error(), "template name is required") {
		t.Errorf("err = %q, want it to carry the message of jsreport", err)
	}
}

func TestGetTemplatesReadsTheTemplateList(t *testing.T) {
	client := stubJSReport(t, http.StatusOK,
		`{"value":[{"_id":"t1","name":"Consumption","recipe":"chrome-pdf"},{"_id":"t2","name":"Sheet","recipe":"xlsx"}]}`)

	templates, err := client.GetTemplates("Bearer t")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(templates) != 2 {
		t.Fatalf("got %d templates, want 2", len(templates))
	}
	if templates[0].Id != "t1" || templates[0].Name != "Consumption" {
		t.Errorf("got %+v, want the first template", templates[0])
	}
	if templates[0].Type != "PDF" {
		t.Errorf("type = %q, want %q", templates[0].Type, "PDF")
	}
	if templates[1].Type != "Excel" {
		t.Errorf("type = %q, want %q", templates[1].Type, "Excel")
	}
}

func TestGetTemplatesReportsAnUnparsableBody(t *testing.T) {
	client := stubJSReport(t, http.StatusOK, "not json")

	if _, err := client.GetTemplates("Bearer t"); err == nil {
		t.Error("got no error for a body that is not json, want one")
	}
}

func TestCreateReportReadsTheHeadersOfTheCreatedFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Report-Id", "f1")
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Permanent-Link", "http://jsreport/reports/f1")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	client := &Client{BaseUrl: srv.URL, HttpClient: resty.New()}

	id, contentType, link, err := client.CreateReport("report", "template", nil, "Bearer t")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "f1" {
		t.Errorf("id = %q, want %q", id, "f1")
	}
	if contentType != "application/pdf" {
		t.Errorf("content type = %q, want %q", contentType, "application/pdf")
	}
	if link != "http://jsreport/reports/f1" {
		t.Errorf("link = %q, want the permanent link", link)
	}
}
