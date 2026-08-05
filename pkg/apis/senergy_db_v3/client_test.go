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

package senergy_db_v3

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/SENERGY-Platform/reporting-service/lib"
	timescaleModels "github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/go-resty/resty/v2"
)

// testClient points a client at a stub that answers with the given body.
func testClient(t *testing.T, status int, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseUrl: srv.URL, HttpClient: resty.New()}
}

func validQuery() timescaleModels.QueriesRequestElement {
	deviceId := "urn:infai:ses:device:0f9a5b8c-1e2d-4a3b-8c7d-6e5f4a3b2c1d"
	// the wrapper insists on a real uuid behind the service prefix
	serviceId := "urn:infai:ses:service:0f9a5b8c-1e2d-4a3b-8c7d-6e5f4a3b2c1d"
	return timescaleModels.QueriesRequestElement{
		DeviceId:  &deviceId,
		ServiceId: &serviceId,
		Columns:   []timescaleModels.QueriesRequestElementColumn{{Name: "energy.value"}},
	}
}

func TestQueryReturnsTheValueColumn(t *testing.T) {
	client := testClient(t, http.StatusOK, `[{"data":[[["2025-01-01T00:00:00Z",12],["2025-01-02T00:00:00Z",13]]]}]`)

	got, err := client.Query("Bearer t", validQuery(), lib.QueryOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []interface{}{float64(12), float64(13)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// A query that matched nothing used to index into an empty slice and take the
// whole service down.
func TestQueryHandlesAnEmptyResult(t *testing.T) {
	cases := map[string]string{
		"no elements at all":     `[]`,
		"element without series": `[{"data":[]}]`,
		"null body":              `null`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			client := testClient(t, http.StatusOK, body)
			got, err := client.Query("Bearer t", validQuery(), lib.QueryOptions{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != 0 {
				t.Errorf("got %v, want no values", got)
			}
		})
	}
}

func TestQueryWithResultObjectArrayReturnsWholeRows(t *testing.T) {
	client := testClient(t, http.StatusOK, `[{"data":[[["2025-01-01T00:00:00Z",12]]]}]`)
	resultObject := "array"

	got, err := client.Query("Bearer t", validQuery(), lib.QueryOptions{ResultObject: &resultObject})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1", len(got))
	}
	row, ok := got[0].([]interface{})
	if !ok {
		t.Fatalf("row is %T, want a slice", got[0])
	}
	if len(row) != 2 {
		t.Errorf("row = %v, want both columns", row)
	}
}

func TestQueryWithResultObjectKeyPicksTheColumn(t *testing.T) {
	client := testClient(t, http.StatusOK, `[{"data":[[["2025-01-01T00:00:00Z",12]]]}]`)
	resultObject := "key"
	resultKey := 0

	got, err := client.Query("Bearer t", validQuery(), lib.QueryOptions{ResultObject: &resultObject, ResultKey: &resultKey})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []interface{}{"2025-01-01T00:00:00Z"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestQueryRejectsAResultKeyOutOfRange(t *testing.T) {
	client := testClient(t, http.StatusOK, `[{"data":[[["2025-01-01T00:00:00Z",12]]]}]`)
	resultObject := "key"
	resultKey := 7

	_, err := client.Query("Bearer t", validQuery(), lib.QueryOptions{ResultObject: &resultObject, ResultKey: &resultKey})
	if err == nil {
		t.Fatal("got no error for a result key past the end of the row, want one")
	}
	if !strings.Contains(err.Error(), "result key") {
		t.Errorf("error = %q, want it to mention the result key", err)
	}
}

func TestQueryRejectsResultObjectKeyWithoutAKey(t *testing.T) {
	client := testClient(t, http.StatusOK, `[{"data":[[["2025-01-01T00:00:00Z",12]]]}]`)
	resultObject := "key"

	if _, err := client.Query("Bearer t", validQuery(), lib.QueryOptions{ResultObject: &resultObject}); err == nil {
		t.Error("got no error for a missing result key, want one")
	}
}

func TestQueryRejectsARowWithoutAValueColumn(t *testing.T) {
	client := testClient(t, http.StatusOK, `[{"data":[[["2025-01-01T00:00:00Z"]]]}]`)

	if _, err := client.Query("Bearer t", validQuery(), lib.QueryOptions{}); err == nil {
		t.Error("got no error for a row without a value column, want one")
	}
}

func TestQueryRejectsAnInvalidRequest(t *testing.T) {
	client := testClient(t, http.StatusOK, `[]`)

	if _, err := client.Query("Bearer t", timescaleModels.QueriesRequestElement{}, lib.QueryOptions{}); err == nil {
		t.Error("got no error for a query without a source, want one")
	}
}

func TestQueryReportsAnUpstreamFailure(t *testing.T) {
	client := testClient(t, http.StatusInternalServerError, `boom`)

	if _, err := client.Query("Bearer t", validQuery(), lib.QueryOptions{}); err == nil {
		t.Error("got no error for a 500 from the timescale wrapper, want one")
	}
}

func TestQueryReportsAnUnparsableResponse(t *testing.T) {
	client := testClient(t, http.StatusOK, `not json`)

	if _, err := client.Query("Bearer t", validQuery(), lib.QueryOptions{}); err == nil {
		t.Error("got no error for a response that is not json, want one")
	}
}
