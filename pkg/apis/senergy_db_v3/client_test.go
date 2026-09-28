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
	"sync/atomic"
	"testing"

	"github.com/SENERGY-Platform/models/go/models"
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

	got, _, err := client.Query("Bearer t", validQuery(), lib.QueryOptions{})
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
			got, _, err := client.Query("Bearer t", validQuery(), lib.QueryOptions{})
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

	got, _, err := client.Query("Bearer t", validQuery(), lib.QueryOptions{ResultObject: &resultObject})
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

	got, _, err := client.Query("Bearer t", validQuery(), lib.QueryOptions{ResultObject: &resultObject, ResultKey: &resultKey})
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

	_, _, err := client.Query("Bearer t", validQuery(), lib.QueryOptions{ResultObject: &resultObject, ResultKey: &resultKey})
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

	if _, _, err := client.Query("Bearer t", validQuery(), lib.QueryOptions{ResultObject: &resultObject}); err == nil {
		t.Error("got no error for a missing result key, want one")
	}
}

func TestQueryRejectsARowWithoutAValueColumn(t *testing.T) {
	client := testClient(t, http.StatusOK, `[{"data":[[["2025-01-01T00:00:00Z"]]]}]`)

	if _, _, err := client.Query("Bearer t", validQuery(), lib.QueryOptions{}); err == nil {
		t.Error("got no error for a row without a value column, want one")
	}
}

func TestQueryRejectsAnInvalidRequest(t *testing.T) {
	client := testClient(t, http.StatusOK, `[]`)

	if _, _, err := client.Query("Bearer t", timescaleModels.QueriesRequestElement{}, lib.QueryOptions{}); err == nil {
		t.Error("got no error for a query without a source, want one")
	}
}

func TestQueryReportsAnUpstreamFailure(t *testing.T) {
	client := testClient(t, http.StatusInternalServerError, `boom`)

	if _, _, err := client.Query("Bearer t", validQuery(), lib.QueryOptions{}); err == nil {
		t.Error("got no error for a 500 from the timescale wrapper, want one")
	}
}

func TestQueryReportsAnUnparsableResponse(t *testing.T) {
	client := testClient(t, http.StatusOK, `not json`)

	if _, _, err := client.Query("Bearer t", validQuery(), lib.QueryOptions{}); err == nil {
		t.Error("got no error for a response that is not json, want one")
	}
}

func groupQuery() timescaleModels.QueriesRequestElement {
	groupId := "urn:infai:ses:device-group:1"
	groupTime := "1d"
	return timescaleModels.QueriesRequestElement{
		DeviceGroupId: &groupId,
		GroupTime:     &groupTime,
		Columns: []timescaleModels.QueriesRequestElementColumn{{
			Criteria: models.DeviceGroupFilterCriteria{FunctionId: "urn:infai:ses:measuring-function:energy", AspectId: "urn:infai:ses:aspect:electricity"},
		}},
	}
}

// One element per device and service, as the wrapper answers a device group query;
// dev-1 matched with two services.
const groupResponse = `[
	{"deviceId":"dev-1","serviceId":"svc-a","columnNames":["p"],"data":[[["2025-01-01T00:00:00Z",1],["2025-01-02T00:00:00Z",2]]]},
	{"deviceId":"dev-1","serviceId":"svc-b","columnNames":["p"],"data":[[["2025-01-01T00:00:00Z",10],["2025-01-02T00:00:00Z",null]]]},
	{"deviceId":"dev-2","serviceId":"svc-a","columnNames":["p"],"data":[[["2025-01-02T00:00:00Z",100]]]}
]`

func TestQueryAggregatesAllElementsOfADeviceGroup(t *testing.T) {
	client := testClient(t, http.StatusOK, groupResponse)

	got, _, err := client.Query("Bearer t", groupQuery(), lib.QueryOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []interface{}{float64(11), float64(102)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestQueryAveragesADeviceGroup(t *testing.T) {
	client := testClient(t, http.StatusOK, groupResponse)
	mean := lib.AggregationMean

	got, _, err := client.Query("Bearer t", groupQuery(), lib.QueryOptions{Aggregation: &mean})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []interface{}{float64(5.5), float64(51)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestQueryMapsAnAggregatedDeviceGroupByResultObject(t *testing.T) {
	cases := []struct {
		name    string
		options lib.QueryOptions
		want    []interface{}
	}{
		{
			name:    "key 0 yields the timestamps",
			options: lib.QueryOptions{ResultObject: strPtr("key"), ResultKey: intPtr(0)},
			want:    []interface{}{"2025-01-01T00:00:00Z", "2025-01-02T00:00:00Z"},
		},
		{
			name:    "key 1 yields the aggregated values",
			options: lib.QueryOptions{ResultObject: strPtr("key"), ResultKey: intPtr(1)},
			want:    []interface{}{float64(11), float64(102)},
		},
		{
			name:    "array yields time and aggregated value rows",
			options: lib.QueryOptions{ResultObject: strPtr("array")},
			want: []interface{}{
				[]interface{}{"2025-01-01T00:00:00Z", float64(11)},
				[]interface{}{"2025-01-02T00:00:00Z", float64(102)},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, http.StatusOK, groupResponse)
			got, _, err := client.Query("Bearer t", groupQuery(), tc.options)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// An aggregated row only has a time and a value column, so a key past them is
// rejected like for a single device.
func TestQueryRejectsAResultKeyPastAnAggregatedRow(t *testing.T) {
	client := testClient(t, http.StatusOK, groupResponse)

	_, _, err := client.Query("Bearer t", groupQuery(), lib.QueryOptions{ResultObject: strPtr("key"), ResultKey: intPtr(2)})
	if err == nil || !strings.Contains(err.Error(), "result key") {
		t.Errorf("error = %v, want a result key error", err)
	}
}

func TestQueryRejectsInvalidDeviceGroupOptions(t *testing.T) {
	cases := map[string]lib.QueryOptions{
		"per device mode":     {DeviceGroupMode: strPtr(lib.DeviceGroupModePerDevice)},
		"unknown mode":        {DeviceGroupMode: strPtr("each")},
		"unknown aggregation": {Aggregation: strPtr("median")},
	}
	for name, options := range cases {
		t.Run(name, func(t *testing.T) {
			client := testClient(t, http.StatusOK, groupResponse)
			if _, _, err := client.Query("Bearer t", groupQuery(), options); err == nil {
				t.Error("got no error, want one")
			}
		})
	}
}

// Without a device group the first element is all that counts, whatever the group
// options say.
func TestQueryOfASingleDeviceIgnoresFurtherElementsAndGroupOptions(t *testing.T) {
	client := testClient(t, http.StatusOK, groupResponse)
	options := lib.QueryOptions{DeviceGroupMode: strPtr(lib.DeviceGroupModePerDevice), Aggregation: strPtr("median")}

	got, _, err := client.Query("Bearer t", validQuery(), options)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []interface{}{float64(1), float64(2)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestQueryPerDeviceKeepsEveryElement(t *testing.T) {
	client := testClient(t, http.StatusOK, groupResponse)

	got, _, err := client.QueryPerDevice("Bearer t", groupQuery(), lib.QueryOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []lib.DeviceSeries{
		{DeviceId: "dev-1", ServiceId: "svc-a", Values: []interface{}{float64(1), float64(2)}},
		{DeviceId: "dev-1", ServiceId: "svc-b", Values: []interface{}{float64(10), nil}},
		{DeviceId: "dev-2", ServiceId: "svc-a", Values: []interface{}{float64(100)}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestQueryPerDeviceMapsRowsByResultObject(t *testing.T) {
	client := testClient(t, http.StatusOK, groupResponse)

	got, _, err := client.QueryPerDevice("Bearer t", groupQuery(), lib.QueryOptions{ResultObject: strPtr("array")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []interface{}{[]interface{}{"2025-01-02T00:00:00Z", float64(100)}}
	if len(got) != 3 || !reflect.DeepEqual(got[2].Values, want) {
		t.Errorf("got %+v, want the last entry's values to be %v", got, want)
	}
}

func TestQueryPerDeviceNamesTheDeviceOfARowItCannotMap(t *testing.T) {
	client := testClient(t, http.StatusOK, `[{"deviceId":"dev-9","serviceId":"svc-a","data":[[["2025-01-01T00:00:00Z"]]]}]`)

	_, _, err := client.QueryPerDevice("Bearer t", groupQuery(), lib.QueryOptions{})
	if err == nil || !strings.Contains(err.Error(), "dev-9") {
		t.Errorf("error = %v, want it to name dev-9", err)
	}
}

func TestQueryPerDeviceRejectsAQueryWithoutDeviceGroup(t *testing.T) {
	client := testClient(t, http.StatusOK, groupResponse)

	if _, _, err := client.QueryPerDevice("Bearer t", validQuery(), lib.QueryOptions{}); err == nil {
		t.Error("got no error for a single device query, want one")
	}
}

func intPtr(i int) *int { return &i }

func TestQueryCountsTheRowsItFetched(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		query timescaleModels.QueriesRequestElement
		want  int
	}{
		{name: "every row of every element of a group", body: groupResponse, query: groupQuery(), want: 5},
		{
			name:  "series of a group that the aggregate does not use",
			body:  `[{"deviceId":"dev-1","data":[[["2025-01-01T00:00:00Z",1]],[["2025-01-01T00:00:00Z",2],["2025-01-02T00:00:00Z",3]]]}]`,
			query: groupQuery(),
			want:  3,
		},
		{name: "the rows of the first series of a single device", body: groupResponse, query: validQuery(), want: 2},
		{name: "nothing for an empty result", body: `[]`, query: validQuery(), want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, http.StatusOK, tc.body)
			_, fetched, err := client.Query("Bearer t", tc.query, lib.QueryOptions{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fetched != tc.want {
				t.Errorf("fetched = %d, want %d", fetched, tc.want)
			}
		})
	}
}

func TestQueryPerDeviceCountsTheRowsItFetched(t *testing.T) {
	client := testClient(t, http.StatusOK, groupResponse)

	_, fetched, err := client.QueryPerDevice("Bearer t", groupQuery(), lib.QueryOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fetched != 5 {
		t.Errorf("fetched = %d, want 5", fetched)
	}
}

// Rows sorted by a value column cannot be merged, so the aggregate is refused; per
// device keeps the order of each series.
func TestQueryRejectsAnAggregateOrderedByAValueColumn(t *testing.T) {
	client := testClient(t, http.StatusOK, groupResponse)
	query := groupQuery()
	query.OrderColumnIndex = intPtr(1)

	_, _, err := client.Query("Bearer t", query, lib.QueryOptions{})
	if err == nil || !strings.Contains(err.Error(), "orderColumnIndex") {
		t.Errorf("error = %v, want one about orderColumnIndex", err)
	}
	if _, _, err := client.QueryPerDevice("Bearer t", query, lib.QueryOptions{}); err != nil {
		t.Errorf("per device: unexpected error %v", err)
	}
}

// An aggregate that cannot be computed is refused without querying the wrapper.
func TestQueryRejectsAnUnmergeableAggregateBeforeFetching(t *testing.T) {
	cases := map[string]func(*timescaleModels.QueriesRequestElement){
		"no group time and no limit": func(q *timescaleModels.QueriesRequestElement) { q.GroupTime = nil },
		"no group time and limit 5": func(q *timescaleModels.QueriesRequestElement) {
			q.GroupTime = nil
			q.Limit = intPtr(5)
		},
		"ordered by a value column": func(q *timescaleModels.QueriesRequestElement) { q.OrderColumnIndex = intPtr(1) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				_, _ = w.Write([]byte(groupResponse))
			}))
			t.Cleanup(srv.Close)
			client := &Client{BaseUrl: srv.URL, HttpClient: resty.New()}
			query := groupQuery()
			mutate(&query)

			if _, _, err := client.Query("Bearer t", query, lib.QueryOptions{}); err == nil {
				t.Error("got no error, want one")
			}
			if n := requests.Load(); n != 0 {
				t.Errorf("sent %d requests, want none", n)
			}
		})
	}
}

func TestQueryMergesTheLatestValueOfEveryDeviceWithoutAGroupTime(t *testing.T) {
	client := testClient(t, http.StatusOK, `[
		{"deviceId":"dev-1","data":[[["2025-01-02T00:00:00Z",1]]]},
		{"deviceId":"dev-2","data":[[["2025-01-01T00:00:00Z",2]]]}
	]`)
	query := groupQuery()
	query.GroupTime = nil
	query.Limit = intPtr(1)

	got, _, err := client.Query("Bearer t", query, lib.QueryOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []interface{}{float64(3)}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// per_device keeps each series as the wrapper limited it, so it needs no groupTime.
func TestQueryPerDeviceAcceptsAQueryWithoutGroupTime(t *testing.T) {
	client := testClient(t, http.StatusOK, groupResponse)
	query := groupQuery()
	query.GroupTime = nil

	if _, _, err := client.QueryPerDevice("Bearer t", query, lib.QueryOptions{}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
