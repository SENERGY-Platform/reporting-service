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
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SENERGY-Platform/reporting-service/lib"
	"github.com/SENERGY-Platform/reporting-service/pkg/apis/device_repository"
	"github.com/SENERGY-Platform/reporting-service/pkg/apis/figures"
	"github.com/SENERGY-Platform/reporting-service/pkg/config"
	"github.com/go-resty/resty/v2"
)

func berlinTime(t *testing.T, value string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04", value, loc)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestResolvePeriod(t *testing.T) {
	cases := []struct {
		name   string
		period lib.FiguresPeriod
		now    string
		start  string
		end    string
		first  string
		last   string
	}{
		{"previous day", lib.FiguresPeriod{Kind: "previousDay"}, "2026-11-23 15:30",
			"2026-11-22T00:00:00+01:00", "2026-11-23T00:00:00+01:00", "2026-11-22", "2026-11-22"},
		{"previous day over the new year", lib.FiguresPeriod{Kind: "previousDay"}, "2026-01-01 00:05",
			"2025-12-31T00:00:00+01:00", "2026-01-01T00:00:00+01:00", "2025-12-31", "2025-12-31"},
		{"previous week on a monday", lib.FiguresPeriod{Kind: "previousWeek"}, "2026-11-23 09:00",
			"2026-11-16T00:00:00+01:00", "2026-11-23T00:00:00+01:00", "2026-11-16", "2026-11-22"},
		{"previous week on a sunday", lib.FiguresPeriod{Kind: "previousWeek"}, "2026-11-22 23:59",
			"2026-11-09T00:00:00+01:00", "2026-11-16T00:00:00+01:00", "2026-11-09", "2026-11-15"},
		{"previous week over the new year", lib.FiguresPeriod{Kind: "previousWeek"}, "2026-01-02 10:00",
			"2025-12-22T00:00:00+01:00", "2025-12-29T00:00:00+01:00", "2025-12-22", "2025-12-28"},
		{"previous month", lib.FiguresPeriod{Kind: "previousMonth"}, "2026-11-23 15:30",
			"2026-10-01T00:00:00+02:00", "2026-11-01T00:00:00+01:00", "2026-10-01", "2026-10-31"},
		{"previous month over the new year", lib.FiguresPeriod{Kind: "previousMonth"}, "2026-01-15 08:00",
			"2025-12-01T00:00:00+01:00", "2026-01-01T00:00:00+01:00", "2025-12-01", "2025-12-31"},
		{"march with the change to summer time", lib.FiguresPeriod{Kind: "previousMonth"}, "2026-04-10 08:00",
			"2026-03-01T00:00:00+01:00", "2026-04-01T00:00:00+02:00", "2026-03-01", "2026-03-31"},
		{"october with the change to winter time", lib.FiguresPeriod{Kind: "previousMonth"}, "2026-11-02 08:00",
			"2026-10-01T00:00:00+02:00", "2026-11-01T00:00:00+01:00", "2026-10-01", "2026-10-31"},
		{"previous quarter", lib.FiguresPeriod{Kind: "previousQuarter"}, "2026-11-23 15:30",
			"2026-07-01T00:00:00+02:00", "2026-10-01T00:00:00+02:00", "2026-07-01", "2026-09-30"},
		{"previous quarter on the first day of a quarter", lib.FiguresPeriod{Kind: "previousQuarter"}, "2026-07-01 00:10",
			"2026-04-01T00:00:00+02:00", "2026-07-01T00:00:00+02:00", "2026-04-01", "2026-06-30"},
		{"previous quarter over the new year", lib.FiguresPeriod{Kind: "previousQuarter"}, "2026-02-10 12:00",
			"2025-10-01T00:00:00+02:00", "2026-01-01T00:00:00+01:00", "2025-10-01", "2025-12-31"},
		{"previous year", lib.FiguresPeriod{Kind: "previousYear"}, "2026-11-23 15:30",
			"2025-01-01T00:00:00+01:00", "2026-01-01T00:00:00+01:00", "2025-01-01", "2025-12-31"},
		{"current month ends at the start of today", lib.FiguresPeriod{Kind: "currentMonth"}, "2026-11-23 15:30",
			"2026-11-01T00:00:00+01:00", "2026-11-23T00:00:00+01:00", "2026-11-01", "2026-11-22"},
		{"current year ends at the start of today", lib.FiguresPeriod{Kind: "currentYear"}, "2026-11-23 15:30",
			"2026-01-01T00:00:00+01:00", "2026-11-23T00:00:00+01:00", "2026-01-01", "2026-11-22"},
		{"fixed includes the end day across the change to summer time", lib.FiguresPeriod{Kind: "fixed", Start: strPtr("2026-03-28"), End: strPtr("2026-03-30")}, "2026-11-23 15:30",
			"2026-03-28T00:00:00+01:00", "2026-03-31T00:00:00+02:00", "2026-03-28", "2026-03-30"},
		{"fixed single day", lib.FiguresPeriod{Kind: "fixed", Start: strPtr("2026-10-25"), End: strPtr("2026-10-25")}, "2026-11-23 15:30",
			"2026-10-25T00:00:00+02:00", "2026-10-26T00:00:00+01:00", "2026-10-25", "2026-10-25"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			window, err := resolvePeriod(tc.period, berlinTime(t, tc.now))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := window.start.Format(time.RFC3339); got != tc.start {
				t.Errorf("start = %s, want %s", got, tc.start)
			}
			if got := window.end.Format(time.RFC3339); got != tc.end {
				t.Errorf("end = %s, want %s", got, tc.end)
			}
			resolved := window.resolved()
			if resolved.Start != tc.first || resolved.End != tc.last {
				t.Errorf("days = %s to %s, want %s to %s", resolved.Start, resolved.End, tc.first, tc.last)
			}
			if resolved.TimeZone != "Europe/Berlin" || resolved.Kind != tc.period.Kind {
				t.Errorf("resolved = %+v", resolved)
			}
		})
	}
}

func TestResolvePeriodMonthsAcrossTheClockChangeAreNotCutAtTwentyFourHours(t *testing.T) {
	hours := func(now string) float64 {
		window, err := resolvePeriod(lib.FiguresPeriod{Kind: "previousMonth"}, berlinTime(t, now))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return window.end.Sub(window.start).Hours()
	}
	if got := hours("2026-04-10 08:00"); got != 31*24-1 {
		t.Errorf("march has %v hours, want %d", got, 31*24-1)
	}
	if got := hours("2026-11-02 08:00"); got != 31*24+1 {
		t.Errorf("october has %v hours, want %d", got, 31*24+1)
	}
}

func TestResolvePeriodCutsDaysInTheGivenZone(t *testing.T) {
	// 23:30 UTC is already the next day in Berlin and still the same day in UTC.
	now := time.Date(2026, 11, 22, 23, 30, 0, 0, time.UTC)
	berlin, err := resolvePeriod(lib.FiguresPeriod{Kind: "previousDay"}, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := berlin.resolved().Start; got != "2026-11-22" {
		t.Errorf("berlin day = %s, want 2026-11-22", got)
	}
	utc, err := resolvePeriod(lib.FiguresPeriod{Kind: "previousDay", TimeZone: strPtr("UTC")}, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := utc.start.Format(time.RFC3339); got != "2026-11-21T00:00:00Z" {
		t.Errorf("utc start = %s, want 2026-11-21T00:00:00Z", got)
	}
	if utc.resolved().TimeZone != "UTC" {
		t.Errorf("zone = %s, want UTC", utc.resolved().TimeZone)
	}
}

func TestResolvePeriodRejects(t *testing.T) {
	cases := []struct {
		name   string
		period lib.FiguresPeriod
		now    string
		want   string
	}{
		{"no kind", lib.FiguresPeriod{}, "2026-11-23 10:00", "needs a kind"},
		{"unknown kind", lib.FiguresPeriod{Kind: "lastDecade"}, "2026-11-23 10:00", "unknown period kind"},
		{"unknown zone", lib.FiguresPeriod{Kind: "previousDay", TimeZone: strPtr("Mars/Olympus")}, "2026-11-23 10:00", "unknown time zone"},
		{"fixed without end", lib.FiguresPeriod{Kind: "fixed", Start: strPtr("2026-01-01")}, "2026-11-23 10:00", "needs start and end"},
		{"fixed with a bad day", lib.FiguresPeriod{Kind: "fixed", Start: strPtr("1.1.2026"), End: strPtr("2026-01-02")}, "2026-11-23 10:00", "YYYY-MM-DD"},
		{"fixed with the end before the start", lib.FiguresPeriod{Kind: "fixed", Start: strPtr("2026-02-02"), End: strPtr("2026-02-01")}, "2026-11-23 10:00", "before its start"},
		{"current month on its first day", lib.FiguresPeriod{Kind: "currentMonth"}, "2026-11-01 10:00", "no completed day"},
		{"current year on its first day", lib.FiguresPeriod{Kind: "currentYear"}, "2026-01-01 10:00", "no completed day"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolvePeriod(tc.period, berlinTime(t, tc.now))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

// The graph and the totals are written by hand in the shape of the two services'
// answers, with the numbers figures' edge routing gives: a node without a meter of
// its own is the sum of its children at their weights, so the hall is 300 kWh of
// electricity and 80 percent of 60 m3 of gas. The site holds a grid meter and a hall,
// a detached meter, an excluded meter, a gas meter whose readings start late, a meter
// without readings and a grid meter with a draw and a feed-in register.
const figuresGraph = `{
  "id": "graph-1",
  "nodes": [
    {"id": "n-site", "resource_type": "", "attributes": [{"key": "name", "value": "Werk"}]},
    {"id": "n-grid", "resource_type": "device", "resource_id": "dev-grid", "attributes": [{"key": "name", "value": "Netzanschluss"}]},
    {"id": "n-hall", "resource_type": "", "attributes": [{"key": "name", "value": "Halle"}]},
    {"id": "n-gas", "resource_type": "device", "resource_id": "dev-gas", "attributes": [{"key": "name", "value": "Gaszaehler"}]},
    {"id": "n-power", "resource_type": "device", "resource_id": "dev-power", "attributes": [{"key": "name", "value": "Strom Halle"}]},
    {"id": "n-idle", "resource_type": "device", "resource_id": "dev-idle"},
    {"id": "n-ex", "resource_type": "device", "resource_id": "dev-ex", "attributes": [{"key": "name", "value": "Ausgeschlossen"}, {"key": "dashboard/excluded", "value": "true"}]},
    {"id": "n-off", "resource_type": "device", "resource_id": "dev-off", "attributes": [{"key": "name", "value": "Abgekoppelt"}]}
  ],
  "edges": [
    {"from_node_id": "n-grid", "to_node_id": "n-site", "weight": 100},
    {"from_node_id": "n-hall", "to_node_id": "n-site", "weight": 100},
    {"from_node_id": "n-power", "to_node_id": "n-hall", "weight": 100},
    {"from_node_id": "n-gas", "to_node_id": "n-hall", "weight": 80},
    {"from_node_id": "n-idle", "to_node_id": "n-hall", "weight": 100},
    {"from_node_id": "n-ex", "to_node_id": "n-hall", "weight": 100},
    {"from_node_id": "n-off", "to_node_id": "n-site", "weight": 0}
  ]
}`

const figuresTotals = `{
  "graphId": "graph-1",
  "range": {"start": "2026-10-01T00:00:00+02:00", "end": "2026-11-01T00:00:00+01:00"},
  "readingsByDevice": {
    "dev-grid": [
      {"carrier": "electricity", "direction": "draw", "first": {"at": "2026-10-01T00:00:10+02:00", "value": 10}, "last": {"at": "2026-10-31T23:59:50+01:00", "value": 510.5}},
      {"carrier": "electricity", "direction": "feedIn", "last": {"at": "2026-10-31T23:59:50+01:00", "value": 4}}
    ],
    "dev-power": [{"carrier": "electricity", "first": {"at": "2026-10-01T00:01:00+02:00", "value": 1}, "last": {"at": "2026-10-31T23:58:00+01:00", "value": 301}}],
    "dev-gas": [{"carrier": "gas", "first": {"at": "2026-10-12T08:00:00+02:00", "value": 5}, "last": {"at": "2026-10-31T23:00:00+01:00", "value": 65}}],
    "dev-ex": [{"carrier": "electricity", "first": {"at": "2026-10-01T00:01:00+02:00", "value": 1}, "last": {"at": "2026-10-31T23:58:00+01:00", "value": 9}}]
  },
  "totals": {
    "n-site": {"electricity": 800.5, "gas": 48},
    "n-grid": {"electricity": 500.5},
    "n-hall": {"electricity": 300, "gas": 48},
    "n-power": {"electricity": 300},
    "n-gas": {"gas": 60}
  }
}`

func recordedFigures(t *testing.T) (device_repository.Graph, figures.TotalsResponse) {
	t.Helper()
	var graph device_repository.Graph
	if err := json.Unmarshal([]byte(figuresGraph), &graph); err != nil {
		t.Fatal(err)
	}
	var totals figures.TotalsResponse
	if err := json.Unmarshal([]byte(figuresTotals), &totals); err != nil {
		t.Fatal(err)
	}
	return graph, totals
}

func octoberWindow(t *testing.T) periodWindow {
	t.Helper()
	window, err := resolvePeriod(lib.FiguresPeriod{Kind: "previousMonth"}, berlinTime(t, "2026-11-23 15:30"))
	if err != nil {
		t.Fatal(err)
	}
	return window
}

func jsonOf(t *testing.T, value any) any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBuildFiguresResultMapsTheRecordedAnswerToTheDocumentedForm(t *testing.T) {
	graph, totals := recordedFigures(t)
	query := lib.FiguresQuery{GraphId: "graph-1"}
	now := berlinTime(t, "2026-11-23 15:30")

	got, err := buildFiguresResult(graph, totals, query, nil, octoberWindow(t), now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := `{
	  "graph": {"id": "graph-1", "name": "Werk"},
	  "node": null,
	  "period": {"kind": "previousMonth", "start": "2026-10-01", "end": "2026-10-31", "timeZone": "Europe/Berlin"},
	  "carriers": ["electricity", "gas"],
	  "rows": [
	    {"nodeId": "n-off", "name": "Abgekoppelt", "path": ["Abgekoppelt"], "depth": 0, "values": {}, "share": null, "alsoUnder": []},
	    {"nodeId": "n-site", "name": "Werk", "path": ["Werk"], "depth": 0, "values": {"electricity": 800.5, "gas": 48}, "share": null, "alsoUnder": []},
	    {"nodeId": "n-hall", "name": "Halle", "path": ["Werk", "Halle"], "depth": 1, "values": {"electricity": 300, "gas": 48}, "share": 100, "alsoUnder": []},
	    {"nodeId": "n-gas", "name": "Gaszaehler", "path": ["Werk", "Halle", "Gaszaehler"], "depth": 2, "values": {"gas": 60}, "share": 80, "alsoUnder": []},
	    {"nodeId": "n-idle", "name": "n-idle", "path": ["Werk", "Halle", "n-idle"], "depth": 2, "values": {}, "share": 100, "alsoUnder": []},
	    {"nodeId": "n-power", "name": "Strom Halle", "path": ["Werk", "Halle", "Strom Halle"], "depth": 2, "values": {"electricity": 300}, "share": 100, "alsoUnder": []},
	    {"nodeId": "n-grid", "name": "Netzanschluss", "path": ["Werk", "Netzanschluss"], "depth": 1, "values": {"electricity": 500.5}, "share": 100, "alsoUnder": []}
	  ],
	  "meters": [
	    {"nodeId": "n-off", "deviceId": "dev-off", "name": "Abgekoppelt", "status": "none"},
	    {"nodeId": "n-gas", "deviceId": "dev-gas", "name": "Gaszaehler", "status": "partial", "first": "2026-10-12", "last": "2026-10-31"},
	    {"nodeId": "n-idle", "deviceId": "dev-idle", "name": "n-idle", "status": "none"},
	    {"nodeId": "n-power", "deviceId": "dev-power", "name": "Strom Halle", "status": "complete", "first": "2026-10-01", "last": "2026-10-31"},
	    {"nodeId": "n-grid", "deviceId": "dev-grid", "name": "Netzanschluss", "status": "complete", "first": "2026-10-01", "last": "2026-10-31"}
	  ],
	  "generatedAt": "2026-11-23T15:30:00+01:00"
	}`
	var wantValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatal(err)
	}
	if gotValue := jsonOf(t, got); !reflect.DeepEqual(gotValue, wantValue) {
		gotRaw, _ := json.MarshalIndent(gotValue, "", " ")
		t.Errorf("got\n%s", gotRaw)
	}
}

func TestBuildFiguresResultKeepsAnUnmeasuredCarrierOutOfValues(t *testing.T) {
	graph, totals := recordedFigures(t)
	got, err := buildFiguresResult(graph, totals, lib.FiguresQuery{GraphId: "graph-1"}, nil, octoberWindow(t), time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, row := range got.Rows {
		if row.NodeId != "n-power" {
			continue
		}
		if _, present := row.Values["gas"]; present {
			t.Errorf("n-power never measured gas, values = %v", row.Values)
		}
	}
}

func TestBuildFiguresResultCutsToTheRequestedCarriers(t *testing.T) {
	graph, totals := recordedFigures(t)
	got, err := buildFiguresResult(graph, totals, lib.FiguresQuery{GraphId: "graph-1"}, []string{"gas", "water"}, octoberWindow(t), time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got.Carriers, []string{"gas", "water"}) {
		t.Errorf("carriers = %v, want gas and water as asked", got.Carriers)
	}
	for _, row := range got.Rows {
		for carrier := range row.Values {
			if carrier != "gas" {
				t.Errorf("row %s holds %s", row.NodeId, carrier)
			}
		}
	}
}

func TestBuildFiguresResultNarrowsTheRowsToTheNodeAndWhatIsBelow(t *testing.T) {
	graph, totals := recordedFigures(t)
	query := lib.FiguresQuery{GraphId: "graph-1", NodeId: strPtr("n-hall")}
	got, err := buildFiguresResult(graph, totals, query, nil, octoberWindow(t), time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Node == nil || got.Node.Id != "n-hall" || got.Node.Name != "Halle" {
		t.Errorf("node = %+v", got.Node)
	}
	var ids, depths []any
	for _, row := range got.Rows {
		ids = append(ids, row.NodeId)
		depths = append(depths, row.Depth)
	}
	if !reflect.DeepEqual(ids, []any{"n-hall", "n-gas", "n-idle", "n-power"}) || !reflect.DeepEqual(depths, []any{0, 1, 1, 1}) {
		t.Errorf("rows = %v at depths %v", ids, depths)
	}
	if len(got.Meters) != 3 {
		t.Errorf("meters = %d, want the three below the hall", len(got.Meters))
	}
	if got.Graph.Name != "Werk" {
		t.Errorf("graph name = %q, want Werk", got.Graph.Name)
	}
}

func TestBuildFiguresResultRejectsANodeOutsideTheGraph(t *testing.T) {
	graph, totals := recordedFigures(t)
	_, err := buildFiguresResult(graph, totals, lib.FiguresQuery{GraphId: "graph-1", NodeId: strPtr("nope")}, nil, octoberWindow(t), time.Now())
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("error = %v, want one naming the node", err)
	}
}

func TestBuildFiguresResultListsANodeWithTwoParentsOnce(t *testing.T) {
	graph, totals := recordedFigures(t)
	graph.Edges = append(graph.Edges, device_repository.GraphEdge{FromNodeId: "n-power", ToNodeId: "n-site", Weight: floatPtr(50)})
	got, err := buildFiguresResult(graph, totals, lib.FiguresQuery{GraphId: "graph-1"}, nil, octoberWindow(t), time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	count := 0
	for _, row := range got.Rows {
		if row.NodeId == "n-power" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("n-power appears %d times, want once", count)
	}
}

func TestBuildFiguresResultNamesTheGraphByItsAttributeFirst(t *testing.T) {
	graph, totals := recordedFigures(t)
	graph.Attributes = []device_repository.Attribute{{Key: "name", Value: "Standort Nord"}}
	got, err := buildFiguresResult(graph, totals, lib.FiguresQuery{GraphId: "graph-1"}, nil, octoberWindow(t), time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Graph.Name != "Standort Nord" {
		t.Errorf("graph name = %q", got.Graph.Name)
	}
}

func TestMeterCoverage(t *testing.T) {
	window := octoberWindow(t) // 2026-10-01T00:00+02:00 until 2026-11-01T00:00+01:00
	node := device_repository.GraphNode{Id: "n", ResourceType: "device", ResourceId: "d"}
	reading := func(first, last string) []figures.MeterReading {
		return []figures.MeterReading{{First: &figures.Position{At: first}, Last: &figures.Position{At: last}}}
	}
	cases := []struct {
		name     string
		readings []figures.MeterReading
		status   string
	}{
		{"no readings", nil, lib.CoverageNone},
		{"both ends at the edge", reading("2026-10-01T00:00:00+02:00", "2026-10-31T23:59:00+01:00"), lib.CoverageComplete},
		{"first reading exactly a day late", reading("2026-10-02T00:00:00+02:00", "2026-10-31T23:59:00+01:00"), lib.CoverageComplete},
		{"first reading a second over a day late", reading("2026-10-02T00:00:01+02:00", "2026-10-31T23:59:00+01:00"), lib.CoveragePartial},
		{"last reading exactly a day early", reading("2026-10-01T00:00:00+02:00", "2026-10-31T00:00:00+01:00"), lib.CoverageComplete},
		{"last reading over a day early", reading("2026-10-01T00:00:00+02:00", "2026-10-30T23:59:59+01:00"), lib.CoveragePartial},
		{"unreadable first instant leaves the last", reading("yesterday", "2026-10-31T23:59:00+01:00"), lib.CoverageComplete},
		{"a register without ends", []figures.MeterReading{{}}, lib.CoverageNone},
		{"two registers take the outermost ends", append(
			reading("2026-10-05T00:00:00+02:00", "2026-10-31T23:59:00+01:00"),
			reading("2026-10-01T00:00:00+02:00", "2026-10-20T00:00:00+02:00")...), lib.CoverageComplete},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := meterCoverage(node, tc.readings, window)
			if got.Status != tc.status {
				t.Errorf("status = %s, want %s", got.Status, tc.status)
			}
			if tc.status == lib.CoverageNone && (got.First != "" || got.Last != "") {
				t.Errorf("a meter without data carries days %q to %q", got.First, got.Last)
			}
		})
	}
}

type figuresStub struct {
	mu             sync.Mutex
	graphStatus    int
	totalsStatus   int
	totalsBody     string
	totalsRequest  map[string]any
	authorizations []string
	graphRequests  int
	totalsRequests int
}

func (s *figuresStub) client(t *testing.T, now time.Time) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.authorizations = append(s.authorizations, r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/device-repository/graphs/graph-1":
			s.graphRequests++
			if s.graphStatus != 0 {
				w.WriteHeader(s.graphStatus)
				return
			}
			_, _ = w.Write([]byte(figuresGraph))
		case "/figures/consumption/totals":
			s.totalsRequests++
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &s.totalsRequest)
			if s.totalsStatus != 0 {
				w.WriteHeader(s.totalsStatus)
				_, _ = w.Write([]byte("forbidden"))
				return
			}
			_, _ = w.Write([]byte(s.totalsBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return &Client{
		Figures:          &figures.Client{BaseUrl: srv.URL + "/figures", HttpClient: resty.New()},
		DeviceRepository: &device_repository.Client{BaseUrl: srv.URL, HttpClient: resty.New()},
		now:              func() time.Time { return now },
	}
}

func figuresReportData(query lib.FiguresQuery) map[string]lib.ReportObject {
	return map[string]lib.ReportObject{
		"consumption": {
			DataType:     lib.DataType{Name: "consumption", ValueType: "object"},
			FiguresQuery: &query,
		},
	}
}

func TestSetReportFileDataAsksFiguresForTheResolvedWindow(t *testing.T) {
	stub := &figuresStub{totalsBody: figuresTotals}
	client := stub.client(t, berlinTime(t, "2026-11-23 15:30"))
	token := testToken(t, "user-1")
	data := figuresReportData(lib.FiguresQuery{GraphId: "graph-1", Period: lib.FiguresPeriod{Kind: "previousMonth"}})

	got, err := client.setReportFileData(data, token, "report-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantRequest := map[string]any{
		"graphId":  "graph-1",
		"range":    map[string]any{"start": "2026-10-01T00:00:00+02:00", "end": "2026-11-01T00:00:00+01:00"},
		"timeZone": "Europe/Berlin",
		"routing":  "edges",
		"net":      true,
	}
	if !reflect.DeepEqual(stub.totalsRequest, wantRequest) {
		t.Errorf("figures was asked %v, want %v", stub.totalsRequest, wantRequest)
	}
	for _, auth := range stub.authorizations {
		if auth != token {
			t.Errorf("a call carried authorization %q, want the token of the run", auth)
		}
	}
	result, ok := got["consumption"].(lib.FiguresResult)
	if !ok {
		t.Fatalf("consumption is %T, want lib.FiguresResult", got["consumption"])
	}
	if result.Graph.Name != "Werk" || len(result.Rows) != 7 || len(result.Meters) != 5 {
		t.Errorf("result = %+v", result)
	}
}

func TestSetReportFileDataLeavesOtherObjectsAlone(t *testing.T) {
	stub := &figuresStub{totalsBody: figuresTotals}
	client := stub.client(t, berlinTime(t, "2026-11-23 15:30"))
	data := figuresReportData(lib.FiguresQuery{GraphId: "graph-1", Period: lib.FiguresPeriod{Kind: "previousMonth"}})
	data["title"] = lib.ReportObject{DataType: lib.DataType{Name: "title", ValueType: "string"}, Value: "October"}

	got, err := client.setReportFileData(data, testToken(t, "user-1"), "report-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["title"] != "October" {
		t.Errorf("title = %v, want October", got["title"])
	}
}

func TestSetReportFileDataExplainsARefusalByFigures(t *testing.T) {
	stub := &figuresStub{totalsBody: figuresTotals, totalsStatus: http.StatusForbidden}
	client := stub.client(t, berlinTime(t, "2026-11-23 15:30"))
	data := figuresReportData(lib.FiguresQuery{GraphId: "graph-1", Period: lib.FiguresPeriod{Kind: "previousMonth"}})

	_, err := client.setReportFileData(data, testToken(t, "user-1"), "report-1")
	if err == nil {
		t.Fatal("got no error, want the run to fail")
	}
	for _, want := range []string{"report object consumption", "figures", "may not read graph graph-1", "403"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestSetReportFileDataExplainsARefusalByTheDeviceRepository(t *testing.T) {
	stub := &figuresStub{totalsBody: figuresTotals, graphStatus: http.StatusForbidden}
	client := stub.client(t, berlinTime(t, "2026-11-23 15:30"))
	data := figuresReportData(lib.FiguresQuery{GraphId: "graph-1", Period: lib.FiguresPeriod{Kind: "previousMonth"}})

	_, err := client.setReportFileData(data, testToken(t, "user-1"), "report-1")
	if err == nil || !strings.Contains(err.Error(), "device repository") || !strings.Contains(err.Error(), "403") {
		t.Errorf("error = %v, want one naming the device repository and 403", err)
	}
	if stub.totalsRequests != 0 {
		t.Errorf("figures was asked %d times after the graph was refused", stub.totalsRequests)
	}
}

func TestSetReportFileDataFailsWhenGraphIsGone(t *testing.T) {
	stub := &figuresStub{graphStatus: http.StatusNotFound}
	client := stub.client(t, berlinTime(t, "2026-11-23 15:30"))
	data := figuresReportData(lib.FiguresQuery{GraphId: "graph-1", Period: lib.FiguresPeriod{Kind: "previousMonth"}})

	_, err := client.setReportFileData(data, testToken(t, "user-1"), "report-1")
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error = %v, want one saying the graph does not exist", err)
	}
}

func TestSetReportFileDataFailsWithoutFiguresConfiguration(t *testing.T) {
	client := &Client{}
	data := figuresReportData(lib.FiguresQuery{GraphId: "graph-1", Period: lib.FiguresPeriod{Kind: "previousMonth"}})

	_, err := client.setReportFileData(data, testToken(t, "user-1"), "report-1")
	if err == nil || !strings.Contains(err.Error(), "FIGURES_URL") {
		t.Errorf("error = %v, want one naming FIGURES_URL", err)
	}
}

func TestSetReportFileDataRejectsAnInvalidFiguresQuery(t *testing.T) {
	cases := []struct {
		name  string
		query lib.FiguresQuery
		want  string
	}{
		{"no graph", lib.FiguresQuery{Period: lib.FiguresPeriod{Kind: "previousMonth"}}, "graphId"},
		{"unknown carrier", lib.FiguresQuery{GraphId: "graph-1", Carriers: []string{"steam"}, Period: lib.FiguresPeriod{Kind: "previousMonth"}}, "unknown carrier"},
		{"unknown period", lib.FiguresQuery{GraphId: "graph-1", Period: lib.FiguresPeriod{Kind: "soon"}}, "unknown period kind"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &figuresStub{totalsBody: figuresTotals}
			client := stub.client(t, berlinTime(t, "2026-11-23 15:30"))
			_, err := client.setReportFileData(figuresReportData(tc.query), testToken(t, "user-1"), "report-1")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want one containing %q", err, tc.want)
			}
			if stub.graphRequests+stub.totalsRequests != 0 {
				t.Errorf("a platform service was called for an invalid query")
			}
		})
	}
}

func floatPtr(f float64) *float64 { return &f }

func TestMeterCoverageKeepsTheEndsOfAnotherRegisterWhenOneLacksAnEnd(t *testing.T) {
	window := octoberWindow(t)
	node := device_repository.GraphNode{Id: "n", ResourceType: "device", ResourceId: "d"}
	readings := []figures.MeterReading{
		{Direction: "draw", First: &figures.Position{At: "2026-10-01T00:00:10+02:00"}, Last: &figures.Position{At: "2026-10-30T12:00:00+01:00"}},
		{Direction: "feedIn", Last: &figures.Position{At: "2026-10-31T23:00:00+01:00"}},
	}
	got := meterCoverage(node, readings, window)
	if got.Status != lib.CoverageComplete || got.First != "2026-10-01" || got.Last != "2026-10-31" {
		t.Errorf("meter = %+v, want complete from 2026-10-01 to 2026-10-31", got)
	}
	got = meterCoverage(node, readings[1:], window)
	if got.Status != lib.CoverageComplete || got.First != "" || got.Last != "2026-10-31" {
		t.Errorf("feed-in only: meter = %+v, want complete with a last day only", got)
	}
}

func TestBuildFiguresResultGivesTheShareAndTheOtherParents(t *testing.T) {
	graph, totals := recordedFigures(t)
	// n-power hangs under the hall at 100 percent, and now under the grid meter and
	// the site at 50 percent each, which makes the hall the first parent reached.
	graph.Edges = append(graph.Edges,
		device_repository.GraphEdge{FromNodeId: "n-power", ToNodeId: "n-grid", Weight: floatPtr(50)},
		device_repository.GraphEdge{FromNodeId: "n-power", ToNodeId: "n-site", Weight: floatPtr(50)},
	)
	got, err := buildFiguresResult(graph, totals, lib.FiguresQuery{GraphId: "graph-1"}, nil, octoberWindow(t), time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rows := map[string]lib.FiguresRow{}
	for _, row := range got.Rows {
		rows[row.NodeId] = row
	}
	power := rows["n-power"]
	if len(power.Path) != 3 || power.Share == nil || *power.Share != 100 || !reflect.DeepEqual(power.AlsoUnder, []string{"Netzanschluss", "Werk"}) {
		t.Errorf("n-power = %+v, want under the hall at 100 and also under Netzanschluss and Werk", power)
	}
	if gas := rows["n-gas"]; gas.Share == nil || *gas.Share != 80 || len(gas.AlsoUnder) != 0 {
		t.Errorf("n-gas = %+v, want a share of 80 and no other parent", gas)
	}
	if site := rows["n-site"]; site.Share != nil || len(site.AlsoUnder) != 0 {
		t.Errorf("n-site = %+v, want no share for a top row", site)
	}
}

func TestBuildFiguresResultSplitsANodeBetweenTwoParentsAtFiftyEach(t *testing.T) {
	graph, totals := recordedFigures(t)
	for i := range graph.Edges {
		if graph.Edges[i].FromNodeId == "n-power" {
			graph.Edges[i].Weight = floatPtr(50)
		}
	}
	graph.Edges = append(graph.Edges, device_repository.GraphEdge{FromNodeId: "n-power", ToNodeId: "n-grid", Weight: floatPtr(50)})
	got, err := buildFiguresResult(graph, totals, lib.FiguresQuery{GraphId: "graph-1"}, nil, octoberWindow(t), time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, row := range got.Rows {
		if row.NodeId != "n-power" {
			continue
		}
		if row.Share == nil || *row.Share != 50 || !reflect.DeepEqual(row.AlsoUnder, []string{"Netzanschluss"}) {
			t.Errorf("n-power = %+v, want 50 percent under the hall and also under Netzanschluss", row)
		}
	}
}

func TestBuildFiguresResultLeavesExcludedNodesOut(t *testing.T) {
	graph, totals := recordedFigures(t)
	got, err := buildFiguresResult(graph, totals, lib.FiguresQuery{GraphId: "graph-1"}, nil, octoberWindow(t), time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, row := range got.Rows {
		if row.NodeId == "n-ex" {
			t.Error("the excluded node has a row")
		}
	}
	for _, meter := range got.Meters {
		if meter.NodeId == "n-ex" {
			t.Error("the excluded node has a meter")
		}
	}
}

func TestBuildFiguresResultNamesTheGraphFromANodeWithoutAnyEdgeAbove(t *testing.T) {
	// n-off hangs under the site at weight 0 and is a top row, but the edge still
	// makes it a child, so the name comes from the site and not from "Abgekoppelt".
	graph, totals := recordedFigures(t)
	graph.Nodes = append([]device_repository.GraphNode{graph.Nodes[7]}, graph.Nodes[:7]...)
	got, err := buildFiguresResult(graph, totals, lib.FiguresQuery{GraphId: "graph-1"}, nil, octoberWindow(t), time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Graph.Name != "Werk" {
		t.Errorf("graph name = %q, want Werk", got.Graph.Name)
	}
}

func TestResolvePeriodStartsADayWhereMidnightDoesNotExist(t *testing.T) {
	// Santiago skips 00:00 to 01:00 on 2026-09-06.
	period := lib.FiguresPeriod{Kind: "fixed", Start: strPtr("2026-09-06"), End: strPtr("2026-09-06"), TimeZone: strPtr("America/Santiago")}
	window, err := resolvePeriod(period, time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := window.start.Format(time.RFC3339); got != "2026-09-06T01:00:00-03:00" {
		t.Errorf("start = %s, want 2026-09-06T01:00:00-03:00", got)
	}
	if got := window.end.Format(time.RFC3339); got != "2026-09-07T00:00:00-03:00" {
		t.Errorf("end = %s, want 2026-09-07T00:00:00-03:00", got)
	}
	if resolved := window.resolved(); resolved.Start != "2026-09-06" || resolved.End != "2026-09-06" {
		t.Errorf("days = %s to %s, want 2026-09-06 twice", resolved.Start, resolved.End)
	}

	// The day before ends where that day starts.
	previous, err := resolvePeriod(lib.FiguresPeriod{Kind: "previousDay", TimeZone: strPtr("America/Santiago")}, time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := previous.end.Format(time.RFC3339); got != "2026-09-06T01:00:00-03:00" {
		t.Errorf("previous day ends %s, want 2026-09-06T01:00:00-03:00", got)
	}
}

func TestResolvePeriodRejectsLocalAndAnEmptyZone(t *testing.T) {
	for _, zone := range []string{"Local", ""} {
		_, err := resolvePeriod(lib.FiguresPeriod{Kind: "previousDay", TimeZone: strPtr(zone)}, time.Now())
		if err == nil || !strings.Contains(err.Error(), "unknown time zone") {
			t.Errorf("zone %q: error = %v, want unknown time zone", zone, err)
		}
	}
}

func TestValidateGraphId(t *testing.T) {
	for _, id := range []string{"", ".", "..", "a/b", "a?b", "a#b", "a b", "a\tb", strings.Repeat("x", 257)} {
		if err := validateGraphId(id); err == nil {
			t.Errorf("graph id %q was accepted", id)
		}
	}
	for _, id := range []string{"urn:infai:ses:graph:1", strings.Repeat("x", 256), "a.b"} {
		if err := validateGraphId(id); err != nil {
			t.Errorf("graph id %q refused: %v", id, err)
		}
	}
}

func TestSetReportFileDataChecksTheGraphIdBeforeAnyCall(t *testing.T) {
	stub := &figuresStub{totalsBody: figuresTotals}
	client := stub.client(t, berlinTime(t, "2026-11-23 15:30"))
	data := figuresReportData(lib.FiguresQuery{GraphId: "../x", Period: lib.FiguresPeriod{Kind: "previousMonth"}})
	if _, err := client.setReportFileData(data, testToken(t, "user-1"), "report-1"); err == nil {
		t.Error("got no error for a graph id with a slash")
	}
	if stub.graphRequests+stub.totalsRequests != 0 {
		t.Error("a platform service was called")
	}
}

func TestPlatformErrorAddsStatusAndTheStartOfTheAnswer(t *testing.T) {
	body := "line one\n  line two " + strings.Repeat("x", 400)
	err := platformError("reading the consumption from figures", "g", &figures.StatusError{Status: 500, Body: body})
	msg := err.Error()
	if strings.Contains(msg, "\n") || !strings.Contains(msg, "status 500: line one line two xxx") || !strings.HasSuffix(msg, "...)") {
		t.Errorf("message = %q", msg)
	}
	if len([]rune(msg)) > 300+len("reading the consumption from figures failed: the service answered with an error (status 500: ...)") {
		t.Errorf("answer is not cut: %d runes", len([]rune(msg)))
	}
}

func TestPlatformErrorBlamesTheUrlWhenFiguresAnswers404(t *testing.T) {
	fromFigures := platformError("reading the consumption from figures", "g", &figures.StatusError{Status: 404, Body: "404 page not found"}).Error()
	if !strings.Contains(fromFigures, "FIGURES_URL") || strings.Contains(fromFigures, "does not exist") || !strings.Contains(fromFigures, "404 page not found") {
		t.Errorf("message = %q", fromFigures)
	}
	fromRepository := platformError("reading the graph", "g", &device_repository.StatusError{Status: 404}).Error()
	if !strings.Contains(fromRepository, "graph g does not exist") {
		t.Errorf("message = %q", fromRepository)
	}
}

func TestFiguresTimeoutReachesBothClients(t *testing.T) {
	cfg := &config.Config{Figures: config.FiguresConfig{Url: "http://gateway/figures", Timeout: "30s"}}
	client := NewClient(nil, cfg)
	if got := client.Figures.HttpClient.GetClient().Timeout; got != 30*time.Second {
		t.Errorf("figures timeout = %v, want 30s", got)
	}
	if got := client.DeviceRepository.HttpClient.GetClient().Timeout; got != 30*time.Second {
		t.Errorf("device repository timeout = %v, want 30s", got)
	}
}

func TestFiguresTimeoutMustBePositiveAndFallsBackToTheDefault(t *testing.T) {
	for _, value := range []string{"", "soon", "0s", "-1m"} {
		cfg := &config.Config{Figures: config.FiguresConfig{Timeout: value}}
		if _, err := ParseFiguresTimeout(cfg); err == nil {
			t.Errorf("timeout %q was accepted", value)
		}
		if got := FiguresTimeout(cfg); got != 2*time.Minute {
			t.Errorf("timeout %q falls back to %v, want 2m", value, got)
		}
	}
}
