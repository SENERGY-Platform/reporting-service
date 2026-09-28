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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/reporting-service/lib"
	"github.com/SENERGY-Platform/reporting-service/pkg/apis/device_manager"
	"github.com/SENERGY-Platform/reporting-service/pkg/apis/senergy_db_v3"
	timescaleModels "github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/go-resty/resty/v2"
	"github.com/prometheus/client_golang/prometheus"
)

// dev-1 has a nickname and two matching services, dev-2 only a name, dev-3 is not
// known to the device manager and has no data.
const deviceGroupResponse = `[
	{"deviceId":"dev-1","serviceId":"svc-a","data":[[["2025-01-01T00:00:00Z",1],["2025-01-02T00:00:00Z",null]]]},
	{"deviceId":"dev-1","serviceId":"svc-b","data":[[["2025-01-01T00:00:00Z",10]]]},
	{"deviceId":"dev-2","serviceId":"svc-a","data":[[["2025-01-02T00:00:00Z",100]]]},
	{"deviceId":"dev-3","serviceId":"svc-a","data":[[]]}
]`

var deviceGroupDevices = []models.Device{
	{Id: "dev-1", Name: "meter-1", Attributes: []models.Attribute{{Key: "shared/nickname", Value: "Kitchen"}}},
	{Id: "dev-2", Name: "meter-2"},
}

// groupStub answers the timescale and device manager calls of a report. The device
// manager only returns the devices named in its ids filter, like the real one.
type groupStub struct {
	queryResponse string
	devices       []models.Device
	deviceStatus  int

	requests  atomic.Int32
	mu        sync.Mutex
	idBatches [][]string
}

func (s *groupStub) client(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/db/v3/queries/v2":
			_, _ = w.Write([]byte(s.queryResponse))
		case "/device-manager/devices":
			if s.deviceStatus != 0 {
				w.WriteHeader(s.deviceStatus)
				return
			}
			var ids []string
			if r.URL.Query().Has("ids") {
				ids = strings.Split(r.URL.Query().Get("ids"), ",")
			}
			s.mu.Lock()
			s.idBatches = append(s.idBatches, ids)
			s.mu.Unlock()
			found := []models.Device{}
			for _, device := range s.devices {
				if slices.Contains(ids, device.Id) {
					found = append(found, device)
				}
			}
			_ = json.NewEncoder(w).Encode(found)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return &Client{
		DBClient:      &senergy_db_v3.Client{BaseUrl: srv.URL, HttpClient: resty.New()},
		DeviceManager: &device_manager.Client{BaseUrl: srv.URL, HttpClient: resty.New()},
	}
}

func deviceGroupClient(t *testing.T) (*Client, *atomic.Int32) {
	t.Helper()
	stub := &groupStub{queryResponse: deviceGroupResponse, devices: deviceGroupDevices}
	return stub.client(t), &stub.requests
}

func groupReportObject(valueType string, options *lib.QueryOptions) lib.ReportObject {
	groupId := "urn:infai:ses:device-group:1"
	groupTime := "1d"
	return lib.ReportObject{
		DataType: lib.DataType{Name: "consumption", ValueType: valueType},
		Query: &timescaleModels.QueriesRequestElement{
			DeviceGroupId: &groupId,
			GroupTime:     &groupTime,
			Columns: []timescaleModels.QueriesRequestElementColumn{{
				Criteria: models.DeviceGroupFilterCriteria{FunctionId: "urn:infai:ses:measuring-function:energy", AspectId: "urn:infai:ses:aspect:electricity"},
			}},
		},
		QueryOptions: options,
	}
}

func TestSetReportFileDataListsADeviceGroupPerDevice(t *testing.T) {
	client, _ := deviceGroupClient(t)
	data := map[string]lib.ReportObject{
		"consumption": groupReportObject("array", &lib.QueryOptions{DeviceGroupMode: strPtr(lib.DeviceGroupModePerDevice)}),
	}

	got, err := client.setReportFileData(data, testToken(t, "user-1"), "report-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, err := json.Marshal(got["consumption"])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `[` +
		`{"deviceId":"dev-1","serviceId":"svc-a","name":"Kitchen","values":[1,0]},` +
		`{"deviceId":"dev-1","serviceId":"svc-b","name":"Kitchen","values":[10]},` +
		`{"deviceId":"dev-2","serviceId":"svc-a","name":"meter-2","values":[100]},` +
		`{"deviceId":"dev-3","serviceId":"svc-a","name":"dev-3","values":[]}` +
		`]`
	if string(raw) != want {
		t.Errorf("got  %s\nwant %s", raw, want)
	}
}

func TestSetReportFileDataAggregatesADeviceGroupIntoAnArray(t *testing.T) {
	client, _ := deviceGroupClient(t)
	data := map[string]lib.ReportObject{
		"consumption": groupReportObject("array", &lib.QueryOptions{Aggregation: strPtr(lib.AggregationSum)}),
	}

	got, err := client.setReportFileData(data, testToken(t, "user-1"), "report-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []interface{}{float64(11), float64(100)}
	if !reflect.DeepEqual(got["consumption"], want) {
		t.Errorf("got %v, want %v", got["consumption"], want)
	}
}

// A query sent without queryOptions used to dereference a nil pointer and take the
// service down; it now gets the defaults, which for a group is an aggregated sum.
func TestSetReportFileDataAcceptsAQueryWithoutOptions(t *testing.T) {
	for _, valueType := range []string{"array", "float"} {
		t.Run(valueType, func(t *testing.T) {
			client, _ := deviceGroupClient(t)
			data := map[string]lib.ReportObject{"consumption": groupReportObject(valueType, nil)}

			got, err := client.setReportFileData(data, testToken(t, "user-1"), "report-1")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["consumption"] == nil {
				t.Errorf("got no value, want the aggregated sum")
			}
		})
	}
}

func TestSetReportFileDataTakesTheFirstAggregatedValueForAScalar(t *testing.T) {
	mean := lib.AggregationMean
	client, _ := deviceGroupClient(t)
	data := map[string]lib.ReportObject{
		"consumption": groupReportObject("float", &lib.QueryOptions{Aggregation: &mean}),
	}

	got, err := client.setReportFileData(data, testToken(t, "user-1"), "report-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["consumption"] != float64(5.5) {
		t.Errorf("got %v, want 5.5", got["consumption"])
	}
}

func TestSetReportFileDataRejectsPerDeviceForAScalar(t *testing.T) {
	client, requests := deviceGroupClient(t)
	data := map[string]lib.ReportObject{
		"consumption": groupReportObject("float", &lib.QueryOptions{DeviceGroupMode: strPtr(lib.DeviceGroupModePerDevice)}),
	}

	_, err := client.setReportFileData(data, testToken(t, "user-1"), "report-1")
	if err == nil || !strings.Contains(err.Error(), "array") {
		t.Fatalf("error = %v, want one asking for value type array", err)
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("sent %d requests, want none for a rejected object", n)
	}
}

func TestSetReportFileDataRejectsAnUnknownDeviceGroupMode(t *testing.T) {
	for _, valueType := range []string{"array", "float"} {
		t.Run(valueType, func(t *testing.T) {
			client, _ := deviceGroupClient(t)
			data := map[string]lib.ReportObject{
				"consumption": groupReportObject(valueType, &lib.QueryOptions{DeviceGroupMode: strPtr("each")}),
			}
			if _, err := client.setReportFileData(data, testToken(t, "user-1"), "report-1"); err == nil {
				t.Error("got no error, want one")
			}
		})
	}
}

func TestDeviceDisplayName(t *testing.T) {
	cases := []struct {
		name   string
		device models.Device
		want   string
	}{
		{name: "prefers the nickname", device: models.Device{Name: "meter", Attributes: []models.Attribute{{Key: "other", Value: "x"}, {Key: "shared/nickname", Value: "Kitchen"}}}, want: "Kitchen"},
		{name: "falls back to the name", device: models.Device{Name: "meter"}, want: "meter"},
		{name: "ignores an empty nickname", device: models.Device{Name: "meter", Attributes: []models.Attribute{{Key: "shared/nickname"}}}, want: "meter"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deviceDisplayName(tc.device); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// The metric counts every row the wrapper returned, not the rows left after merging.
func TestSetReportFileDataCountsEveryFetchedRow(t *testing.T) {
	cases := []struct {
		name    string
		options *lib.QueryOptions
	}{
		{name: "aggregate", options: &lib.QueryOptions{}},
		{name: "per device", options: &lib.QueryOptions{DeviceGroupMode: strPtr(lib.DeviceGroupModePerDevice)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := deviceGroupClient(t)
			userId, reportId := "metric-user-"+tc.name, "metric-report-"+tc.name
			data := map[string]lib.ReportObject{"consumption": groupReportObject("array", tc.options)}

			if _, err := client.setReportFileData(data, testToken(t, userId), reportId); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := datapointsCounted(t, userId, reportId); got != 4 {
				t.Errorf("counted %v data points, want the 4 rows of all elements", got)
			}
		})
	}
}

func TestSetReportFileDataFallsBackToIdsWhenNamesCannotBeResolved(t *testing.T) {
	stub := &groupStub{queryResponse: deviceGroupResponse, devices: deviceGroupDevices, deviceStatus: http.StatusInternalServerError}
	client := stub.client(t)
	data := map[string]lib.ReportObject{
		"consumption": groupReportObject("array", &lib.QueryOptions{DeviceGroupMode: strPtr(lib.DeviceGroupModePerDevice)}),
	}

	got, err := client.setReportFileData(data, testToken(t, "user-1"), "report-1")
	if err != nil {
		t.Fatalf("got error %v, want the report without names", err)
	}
	series, ok := got["consumption"].([]lib.DeviceSeries)
	if !ok || len(series) != 4 {
		t.Fatalf("got %#v, want four entries", got["consumption"])
	}
	for _, entry := range series {
		if entry.Name != entry.DeviceId {
			t.Errorf("entry %s is named %q, want its id", entry.DeviceId, entry.Name)
		}
	}
}

// The device list pages at 100 by default, so names are requested by id, in
// batches that keep the url short.
func TestSetReportFileDataNamesMoreDevicesThanOnePage(t *testing.T) {
	const count = 120
	var elements []string
	var devices []models.Device
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("dev-%03d", i)
		elements = append(elements, fmt.Sprintf(`{"deviceId":%q,"serviceId":"svc-a","data":[[["2025-01-01T00:00:00Z",1]]]}`, id))
		devices = append(devices, models.Device{Id: id, Name: "name-" + id})
	}
	stub := &groupStub{queryResponse: "[" + strings.Join(elements, ",") + "]", devices: devices}
	client := stub.client(t)
	data := map[string]lib.ReportObject{
		"consumption": groupReportObject("array", &lib.QueryOptions{DeviceGroupMode: strPtr(lib.DeviceGroupModePerDevice)}),
	}

	got, err := client.setReportFileData(data, testToken(t, "user-1"), "report-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, entry := range got["consumption"].([]lib.DeviceSeries) {
		if entry.Name != "name-"+entry.DeviceId {
			t.Errorf("entry %s is named %q, want %q", entry.DeviceId, entry.Name, "name-"+entry.DeviceId)
		}
	}
	requested := 0
	for _, batch := range stub.idBatches {
		if len(batch) > 50 {
			t.Errorf("one request asked for %d ids, want at most 50", len(batch))
		}
		requested += len(batch)
	}
	if requested != count {
		t.Errorf("asked for %d ids in total, want each of the %d devices once", requested, count)
	}
}

// datapointsCounted reads the data point counter of one user and report.
func datapointsCounted(t *testing.T, userId string, reportId string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("could not gather metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() != "reporting_queried_datapoints_tsdb_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := map[string]string{}
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["user_id"] == userId && labels["report_id"] == reportId {
				return metric.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func TestSetReportFileDataAsksForADeviceWithSeveralServicesOnce(t *testing.T) {
	stub := &groupStub{queryResponse: deviceGroupResponse, devices: deviceGroupDevices}
	client := stub.client(t)
	data := map[string]lib.ReportObject{
		"consumption": groupReportObject("array", &lib.QueryOptions{DeviceGroupMode: strPtr(lib.DeviceGroupModePerDevice)}),
	}

	if _, err := client.setReportFileData(data, testToken(t, "user-1"), "report-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var requested []string
	for _, batch := range stub.idBatches {
		requested = append(requested, batch...)
	}
	if want := []string{"dev-1", "dev-2", "dev-3"}; !reflect.DeepEqual(requested, want) {
		t.Errorf("asked for %v, want %v", requested, want)
	}
}
