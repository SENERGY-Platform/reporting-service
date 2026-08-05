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
	"reflect"
	"testing"
	"time"

	"github.com/SENERGY-Platform/reporting-service/lib"
	timescaleModels "github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"go.mongodb.org/mongo-driver/bson"
)

func TestCalculateNextSchedule(t *testing.T) {
	t.Run("returns no date for a report without a cron", func(t *testing.T) {
		ts, err := calculateNextSchedule(lib.Report{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ts != nil {
			t.Errorf("got %v, want nil", ts)
		}
	})

	t.Run("returns a date in the future for a valid cron", func(t *testing.T) {
		ts, err := calculateNextSchedule(lib.Report{Cron: "*/5 * * * *"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ts == nil {
			t.Fatal("got nil, want a date")
		}
		if !ts.After(time.Now()) {
			t.Errorf("got %v, want a date after now", ts)
		}
	})

	t.Run("rejects an invalid cron", func(t *testing.T) {
		if _, err := calculateNextSchedule(lib.Report{Cron: "not a cron"}); err == nil {
			t.Error("got no error, want one")
		}
	})
}

func TestReportListOptions(t *testing.T) {
	t.Run("sorts descending", func(t *testing.T) {
		opt, err := reportListOptions(map[string][]string{"order": {"createdat:desc"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := bson.D{{Key: "createdat", Value: -1}}
		if !reflect.DeepEqual(opt.Sort, want) {
			t.Errorf("sort = %v, want %v", opt.Sort, want)
		}
	})

	t.Run("sorts ascending", func(t *testing.T) {
		opt, err := reportListOptions(map[string][]string{"order": {"name:asc"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := bson.D{{Key: "name", Value: 1}}
		if !reflect.DeepEqual(opt.Sort, want) {
			t.Errorf("sort = %v, want %v", opt.Sort, want)
		}
	})

	t.Run("reads limit and offset", func(t *testing.T) {
		opt, err := reportListOptions(map[string][]string{"limit": {"10"}, "offset": {"20"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opt.Limit == nil || *opt.Limit != 10 {
			t.Errorf("limit = %v, want 10", opt.Limit)
		}
		if opt.Skip == nil || *opt.Skip != 20 {
			t.Errorf("skip = %v, want 20", opt.Skip)
		}
	})

	// An order without a direction used to index past the end of the split result
	// and take the whole service down.
	t.Run("rejects malformed arguments instead of panicking", func(t *testing.T) {
		cases := map[string]map[string][]string{
			"order without a direction":   {"order": {"createdat"}},
			"order with an empty field":   {"order": {":desc"}},
			"order with a bad direction":  {"order": {"createdat:sideways"}},
			"limit that is not a number":  {"limit": {"ten"}},
			"negative limit":              {"limit": {"-1"}},
			"offset that is not a number": {"offset": {"twenty"}},
			"negative offset":             {"offset": {"-5"}},
		}
		for name, args := range cases {
			t.Run(name, func(t *testing.T) {
				if _, err := reportListOptions(args); err == nil {
					t.Errorf("reportListOptions(%v) succeeded, want an error", args)
				}
			})
		}
	})

	t.Run("accepts an empty argument list", func(t *testing.T) {
		opt, err := reportListOptions(map[string][]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opt.Sort != nil || opt.Limit != nil || opt.Skip != nil {
			t.Errorf("got %+v, want no options set", opt)
		}
	})
}

func TestRollDate(t *testing.T) {
	t.Run("moves the date into the current month keeping the day", func(t *testing.T) {
		got, err := rollDate("2020-03-17T00:00:00Z", "month", intPtr(0))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		parsed, err := time.Parse(time.RFC3339, got)
		if err != nil {
			t.Fatalf("result is not a valid date: %v", err)
		}
		now := time.Now()
		if parsed.Year() != now.Year() || parsed.Month() != now.Month() {
			t.Errorf("got %v, want the current month", parsed)
		}
		if parsed.Day() != 17 {
			t.Errorf("day = %d, want 17", parsed.Day())
		}
	})

	t.Run("moves the date into the current year keeping month and day", func(t *testing.T) {
		got, err := rollDate("2020-03-17T00:00:00Z", "year", intPtr(0))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		parsed, _ := time.Parse(time.RFC3339, got)
		if parsed.Year() != time.Now().Year() {
			t.Errorf("year = %d, want %d", parsed.Year(), time.Now().Year())
		}
		if parsed.Month() != time.March || parsed.Day() != 17 {
			t.Errorf("got %v, want March 17th", parsed)
		}
	})

	t.Run("keeps the offset that the stored date carries", func(t *testing.T) {
		// 120 minutes offset means the stored date is 02:00 of the intended day.
		got, err := rollDate("2020-03-17T02:00:00Z", "month", intPtr(120))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		parsed, _ := time.Parse(time.RFC3339, got)
		if parsed.Hour() != 2 || parsed.Day() != 17 {
			t.Errorf("got %v, want the 17th at 02:00", parsed)
		}
	})

	t.Run("rejects a date that is not RFC3339", func(t *testing.T) {
		if _, err := rollDate("17.03.2020", "month", intPtr(0)); err == nil {
			t.Error("got no error, want one")
		}
	})
}

func TestUpdateStartAndEndDate(t *testing.T) {
	client := &Client{}

	// A query may legitimately arrive without a time range or without offsets, and
	// both used to be dereferenced unconditionally.
	t.Run("ignores a query without a time range", func(t *testing.T) {
		object := lib.ReportObject{
			Query:        &timescaleModels.QueriesRequestElement{},
			QueryOptions: &lib.QueryOptions{RollingStartDate: strPtr("month")},
		}
		if err := client.updateStartAndEndDate(&object); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ignores an object without a query", func(t *testing.T) {
		object := lib.ReportObject{
			QueryOptions: &lib.QueryOptions{RollingStartDate: strPtr("month")},
		}
		if err := client.updateStartAndEndDate(&object); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rolls the start date without an offset being set", func(t *testing.T) {
		start := "2020-03-17T00:00:00Z"
		object := lib.ReportObject{
			Query:        &timescaleModels.QueriesRequestElement{Time: &timescaleModels.QueriesRequestElementTime{Start: &start}},
			QueryOptions: &lib.QueryOptions{RollingStartDate: strPtr("month")},
		}
		if err := client.updateStartAndEndDate(&object); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if start == "2020-03-17T00:00:00Z" {
			t.Error("start date was not rolled forward")
		}
	})

	t.Run("reports a start date that cannot be parsed", func(t *testing.T) {
		start := "not a date"
		object := lib.ReportObject{
			Query:        &timescaleModels.QueriesRequestElement{Time: &timescaleModels.QueriesRequestElementTime{Start: &start}},
			QueryOptions: &lib.QueryOptions{RollingStartDate: strPtr("month")},
		}
		if err := client.updateStartAndEndDate(&object); err == nil {
			t.Error("got no error, want one")
		}
	})

	t.Run("reports an end date that cannot be parsed", func(t *testing.T) {
		end := "not a date"
		object := lib.ReportObject{
			Query:        &timescaleModels.QueriesRequestElement{Time: &timescaleModels.QueriesRequestElementTime{End: &end}},
			QueryOptions: &lib.QueryOptions{RollingEndDate: strPtr("month")},
		}
		if err := client.updateStartAndEndDate(&object); err == nil {
			t.Error("got no error, want one")
		}
	})
}

func TestFilterQueryValues(t *testing.T) {
	client := &Client{}
	got := client.filterQueryValues([]interface{}{1.0, nil, 3.0})
	want := []interface{}{1.0, 0, 3.0}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func intPtr(i int) *int       { return &i }
func strPtr(s string) *string { return &s }
