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

package lib

import (
	"encoding/json"
	"testing"
	"time"
)

// The job document doubles as the api response, so the fields that only the worker
// needs must not be serialized.
func TestReportJobKeepsInternalFieldsOutOfJSON(t *testing.T) {
	now := time.Now()
	job := ReportJob{
		Id:        "job-1",
		ReportId:  "report-1",
		Status:    ReportJobRunning,
		Step:      ReportJobStepRendering,
		CreatedAt: now,
		StartedAt: &now,
		UserId:    "user-1",
		Request:   Report{Id: "report-1", Name: "Verbrauch"},
		SendEmail: true,
		Heartbeat: &now,
	}

	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var decoded map[string]any
	if err = json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, field := range []string{"userId", "UserId", "request", "Request", "sendEmail", "SendEmail", "heartbeat", "Heartbeat"} {
		if _, present := decoded[field]; present {
			t.Errorf("internal field %q is serialized: %s", field, raw)
		}
	}
	for _, field := range []string{"id", "reportId", "status", "step", "createdAt", "startedAt"} {
		if _, present := decoded[field]; !present {
			t.Errorf("field %q is missing from the response: %s", field, raw)
		}
	}
	// an unfinished job must not claim a finish date
	if _, present := decoded["finishedAt"]; present {
		t.Errorf("finishedAt is present on a running job: %s", raw)
	}
}

func TestReportJobDone(t *testing.T) {
	cases := map[ReportJobStatus]bool{
		ReportJobPending: false,
		ReportJobRunning: false,
		ReportJobDone:    true,
		ReportJobFailed:  true,
	}
	for status, want := range cases {
		if got := (ReportJob{Status: status}).Done(); got != want {
			t.Errorf("Done() for status %q = %v, want %v", status, got, want)
		}
	}
}

func TestQueryOptionsGroupMode(t *testing.T) {
	str := func(s string) *string { return &s }
	cases := []struct {
		name    string
		mode    *string
		want    string
		wantErr bool
	}{
		{name: "defaults to aggregate", mode: nil, want: DeviceGroupModeAggregate},
		{name: "accepts aggregate", mode: str("aggregate"), want: DeviceGroupModeAggregate},
		{name: "accepts per_device", mode: str("per_device"), want: DeviceGroupModePerDevice},
		{name: "rejects an empty mode", mode: str(""), wantErr: true},
		{name: "rejects an unknown mode", mode: str("perDevice"), wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := QueryOptions{DeviceGroupMode: tc.mode}.GroupMode()
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestQueryOptionsGroupAggregation(t *testing.T) {
	str := func(s string) *string { return &s }
	cases := []struct {
		name        string
		aggregation *string
		want        string
		wantErr     bool
	}{
		{name: "defaults to sum", aggregation: nil, want: AggregationSum},
		{name: "accepts sum", aggregation: str("sum"), want: AggregationSum},
		{name: "accepts mean", aggregation: str("mean"), want: AggregationMean},
		{name: "rejects an unknown aggregation", aggregation: str("avg"), wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := QueryOptions{Aggregation: tc.aggregation}.GroupAggregation()
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// The web ui is built against these exact keys.
func TestDeviceSeriesJSONShape(t *testing.T) {
	raw, err := json.Marshal(DeviceSeries{DeviceId: "d", ServiceId: "s", Name: "n", Values: []interface{}{1.5}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `{"deviceId":"d","serviceId":"s","name":"n","values":[1.5]}`
	if string(raw) != want {
		t.Errorf("got %s, want %s", raw, want)
	}
}
