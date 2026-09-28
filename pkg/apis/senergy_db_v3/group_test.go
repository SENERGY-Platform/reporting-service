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
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/SENERGY-Platform/reporting-service/lib"
	timescaleModels "github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

const (
	t1 = "2025-01-01T00:00:00Z"
	t2 = "2025-01-02T00:00:00Z"
	t3 = "2025-01-03T00:00:00Z"
)

// series builds a response element for one device with a single series.
func series(deviceId string, rows ...[]interface{}) timescaleModels.QueriesV2ResponseElement {
	return timescaleModels.QueriesV2ResponseElement{DeviceId: &deviceId, Data: [][][]interface{}{rows}}
}

func row(values ...interface{}) []interface{} { return values }

func grouped(mutate ...func(*timescaleModels.QueriesRequestElement)) timescaleModels.QueriesRequestElement {
	groupTime := "1d"
	q := timescaleModels.QueriesRequestElement{GroupTime: &groupTime}
	for _, m := range mutate {
		m(&q)
	}
	return q
}

// latest asks for the latest value of each device, the one case merged by position.
func latest() timescaleModels.QueriesRequestElement {
	return timescaleModels.QueriesRequestElement{Limit: intPtr(1)}
}

func TestAggregateDeviceGroup(t *testing.T) {
	desc := timescaleModels.Desc
	cases := []struct {
		name        string
		elements    []timescaleModels.QueriesV2ResponseElement
		query       timescaleModels.QueriesRequestElement
		aggregation string
		want        [][]interface{}
	}{
		{
			name: "sums rows with equal timestamps and sorts the union ascending",
			elements: []timescaleModels.QueriesV2ResponseElement{
				series("a", row(t1, 1.0), row(t3, 2.0)),
				series("b", row(t2, 10.0), row(t3, 20.0)),
			},
			query:       grouped(),
			aggregation: lib.AggregationSum,
			want:        [][]interface{}{row(t1, 1.0), row(t2, 10.0), row(t3, 22.0)},
		},
		{
			name: "averages rows with equal timestamps",
			elements: []timescaleModels.QueriesV2ResponseElement{
				series("a", row(t1, 1.0), row(t2, 4.0)),
				series("b", row(t1, 3.0)),
			},
			query:       grouped(),
			aggregation: lib.AggregationMean,
			want:        [][]interface{}{row(t1, 2.0), row(t2, 4.0)},
		},
		{
			name: "sorts descending when the query orders descending",
			elements: []timescaleModels.QueriesV2ResponseElement{
				series("a", row(t3, 1.0), row(t1, 1.0)),
				series("b", row(t2, 1.0)),
			},
			query:       grouped(func(q *timescaleModels.QueriesRequestElement) { q.OrderDirection = &desc }),
			aggregation: lib.AggregationSum,
			want:        [][]interface{}{row(t3, 1.0), row(t2, 1.0), row(t1, 1.0)},
		},
		{
			name: "applies the limit to the union of all timestamps",
			elements: []timescaleModels.QueriesV2ResponseElement{
				series("a", row(t3, 1.0)),
				series("b", row(t2, 2.0)),
			},
			query: grouped(func(q *timescaleModels.QueriesRequestElement) {
				limit := 1
				q.Limit = &limit
				q.OrderDirection = &desc
			}),
			aggregation: lib.AggregationSum,
			want:        [][]interface{}{row(t3, 1.0)},
		},
		{
			name: "matches equal instants written with different offsets and keeps the first spelling",
			elements: []timescaleModels.QueriesV2ResponseElement{
				series("a", row("2025-01-01T01:00:00+01:00", 1.0)),
				series("b", row(t1, 2.0)),
			},
			query:       grouped(),
			aggregation: lib.AggregationSum,
			want:        [][]interface{}{row("2025-01-01T01:00:00+01:00", 3.0)},
		},
		{
			name: "combines the latest values without a group time and takes the first element's timestamp",
			elements: []timescaleModels.QueriesV2ResponseElement{
				series("a", row(t1, 1.0)),
				series("b", row(t3, 3.0)),
			},
			query:       latest(),
			aggregation: lib.AggregationSum,
			want:        [][]interface{}{row(t1, 4.0)},
		},
		{
			name: "takes the timestamp of a later element when the first has no latest value",
			elements: []timescaleModels.QueriesV2ResponseElement{
				series("a"),
				series("b", row(t2, 3.0)),
				series("c", row(t3, 5.0)),
			},
			query:       latest(),
			aggregation: lib.AggregationMean,
			want:        [][]interface{}{row(t2, 4.0)},
		},
		{
			name: "ignores nil values in a sum",
			elements: []timescaleModels.QueriesV2ResponseElement{
				series("a", row(t1, nil)),
				series("b", row(t1, 5.0)),
			},
			query:       grouped(),
			aggregation: lib.AggregationSum,
			want:        [][]interface{}{row(t1, 5.0)},
		},
		{
			name: "ignores nil values in a mean instead of counting them as zero",
			elements: []timescaleModels.QueriesV2ResponseElement{
				series("a", row(t1, nil)),
				series("b", row(t1, 4.0)),
				series("c", row(t1, 2.0)),
			},
			query:       grouped(),
			aggregation: lib.AggregationMean,
			want:        [][]interface{}{row(t1, 3.0)},
		},
		{
			name: "keeps a row nil when every device is nil",
			elements: []timescaleModels.QueriesV2ResponseElement{
				series("a", row(t1, nil), row(t2, 1.0)),
				series("b", row(t1, nil)),
			},
			query:       grouped(),
			aggregation: lib.AggregationMean,
			want:        [][]interface{}{row(t1, nil), row(t2, 1.0)},
		},
		{
			name: "uses only the first value column of a row",
			elements: []timescaleModels.QueriesV2ResponseElement{
				series("a", row(t1, 1.0, 100.0)),
				series("b", row(t1, 2.0, 200.0)),
			},
			query:       grouped(),
			aggregation: lib.AggregationSum,
			want:        [][]interface{}{row(t1, 3.0)},
		},
		{
			name: "skips an element without series",
			elements: []timescaleModels.QueriesV2ResponseElement{
				{DeviceId: strPtr("a")},
				series("b", row(t1, 2.0)),
			},
			query:       grouped(),
			aggregation: lib.AggregationSum,
			want:        [][]interface{}{row(t1, 2.0)},
		},
		{
			name: "keeps timestamps apart that differ by less than a second",
			elements: []timescaleModels.QueriesV2ResponseElement{
				series("a", row("2025-01-01T00:00:00.5Z", 1.0)),
				series("b", row(t1, 2.0)),
			},
			query:       grouped(),
			aggregation: lib.AggregationSum,
			want:        [][]interface{}{row(t1, 2.0), row("2025-01-01T00:00:00.5Z", 1.0)},
		},
		{
			name: "averages values whose sum overflows",
			elements: []timescaleModels.QueriesV2ResponseElement{
				series("a", row(t1, math.MaxFloat64)),
				series("b", row(t1, math.MaxFloat64)),
				series("c", row(t1, -math.MaxFloat64)),
			},
			query:       grouped(),
			aggregation: lib.AggregationMean,
			want:        [][]interface{}{row(t1, math.MaxFloat64/3)},
		},
		{
			name: "accepts ordering by the time column",
			elements: []timescaleModels.QueriesV2ResponseElement{
				series("a", row(t1, 1.0)),
				series("b", row(t1, 2.0)),
			},
			query:       grouped(func(q *timescaleModels.QueriesRequestElement) { q.OrderColumnIndex = intPtr(0) }),
			aggregation: lib.AggregationSum,
			want:        [][]interface{}{row(t1, 3.0)},
		},
		{
			name:        "returns no rows for no elements",
			elements:    nil,
			query:       grouped(),
			aggregation: lib.AggregationSum,
			want:        [][]interface{}{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := AggregateDeviceGroup(tc.elements, tc.query, tc.aggregation)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAggregateDeviceGroupRejects(t *testing.T) {
	cases := []struct {
		name        string
		elements    []timescaleModels.QueriesV2ResponseElement
		query       timescaleModels.QueriesRequestElement
		aggregation string
		wantInError string
	}{
		{
			name: "a non-numeric value, naming the device",
			elements: []timescaleModels.QueriesV2ResponseElement{
				series("dev-1", row(t1, 1.0)),
				series("dev-2", row(t1, "on")),
			},
			query:       grouped(),
			aggregation: lib.AggregationSum,
			wantInError: "dev-2",
		},
		{
			name:        "a boolean value",
			elements:    []timescaleModels.QueriesV2ResponseElement{series("dev-1", row(t1, true))},
			query:       latest(),
			aggregation: lib.AggregationSum,
			wantInError: "non-numeric",
		},
		{
			name:        "a row without a value column",
			elements:    []timescaleModels.QueriesV2ResponseElement{series("dev-1", row(t1))},
			query:       latest(),
			aggregation: lib.AggregationSum,
			wantInError: "no value column",
		},
		{
			name:        "a timestamp that is not RFC 3339 when aligning by time",
			elements:    []timescaleModels.QueriesV2ResponseElement{series("dev-1", row("yesterday", 1.0))},
			query:       grouped(),
			aggregation: lib.AggregationSum,
			wantInError: "dev-1",
		},
		{
			name:        "a missing timestamp when aligning by time",
			elements:    []timescaleModels.QueriesV2ResponseElement{series("dev-1", row(nil, 1.0))},
			query:       grouped(),
			aggregation: lib.AggregationSum,
			wantInError: "timestamp",
		},
		{
			name: "a sum that overflows",
			elements: []timescaleModels.QueriesV2ResponseElement{
				series("dev-1", row(t1, math.MaxFloat64)),
				series("dev-2", row(t1, math.MaxFloat64)),
			},
			query:       latest(),
			aggregation: lib.AggregationSum,
			wantInError: "out of range",
		},
		{
			name:        "ordering by a value column with a group time",
			elements:    []timescaleModels.QueriesV2ResponseElement{series("dev-1", row(t1, 1.0))},
			query:       grouped(func(q *timescaleModels.QueriesRequestElement) { q.OrderColumnIndex = intPtr(1) }),
			aggregation: lib.AggregationSum,
			wantInError: "orderColumnIndex",
		},
		{
			name:        "ordering by a value column without a group time",
			elements:    []timescaleModels.QueriesV2ResponseElement{series("dev-1", row(t1, 1.0))},
			query:       timescaleModels.QueriesRequestElement{OrderColumnIndex: intPtr(1), Limit: intPtr(1)},
			aggregation: lib.AggregationMean,
			wantInError: "orderColumnIndex",
		},
		{
			name:        "no group time and no limit",
			elements:    []timescaleModels.QueriesV2ResponseElement{series("dev-1", row(t1, 1.0))},
			query:       timescaleModels.QueriesRequestElement{},
			aggregation: lib.AggregationSum,
			wantInError: "needs a groupTime, or a limit of 1",
		},
		{
			name:        "no group time and a limit other than 1",
			elements:    []timescaleModels.QueriesV2ResponseElement{series("dev-1", row(t1, 1.0))},
			query:       timescaleModels.QueriesRequestElement{Limit: intPtr(5)},
			aggregation: lib.AggregationSum,
			wantInError: "needs a groupTime, or a limit of 1",
		},
		{
			name:        "no group time and a limit of 0",
			elements:    []timescaleModels.QueriesV2ResponseElement{series("dev-1", row(t1, 1.0))},
			query:       timescaleModels.QueriesRequestElement{Limit: intPtr(0)},
			aggregation: lib.AggregationSum,
			wantInError: "needs a groupTime, or a limit of 1",
		},
		{
			name:        "an unknown aggregation",
			elements:    []timescaleModels.QueriesV2ResponseElement{series("dev-1", row(t1, 1.0))},
			query:       latest(),
			aggregation: "median",
			wantInError: "median",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := AggregateDeviceGroup(tc.elements, tc.query, tc.aggregation)
			if err == nil {
				t.Fatal("got no error, want one")
			}
			if !strings.Contains(err.Error(), tc.wantInError) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantInError)
			}
		})
	}
}

func strPtr(s string) *string { return &s }
