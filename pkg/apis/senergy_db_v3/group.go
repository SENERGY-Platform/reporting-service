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
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/SENERGY-Platform/reporting-service/lib"
	timescaleModels "github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

// groupRow accumulates the values that the devices of a group contribute to one row.
type groupRow struct {
	timestamp interface{}
	instant   time.Time
	sum       float64
	// scaledSum is the sum scaled by 2^-meanScale, so a mean stays computable when sum overflows.
	scaledSum float64
	count     int
}

const meanScale = 64

// validateAggregate reports whether a device group query can be aggregated, so it
// can be rejected before anything is fetched.
func validateAggregate(query timescaleModels.QueriesRequestElement, aggregation string) error {
	if aggregation != lib.AggregationSum && aggregation != lib.AggregationMean {
		return errors.New("senergy_db_v3.client - unknown aggregation: " + aggregation)
	}
	// Each series arrives sorted and limited by that column, so combining them would
	// yield rows that belong to no single point in time.
	if query.OrderColumnIndex != nil && *query.OrderColumnIndex != 0 {
		return errors.New("senergy_db_v3.client - an aggregated device group can only be ordered by time (orderColumnIndex 0)")
	}
	// Without time buckets the i-th rows of two devices share nothing but their
	// position, which only means the same thing for the single latest value.
	if query.GroupTime == nil && (query.Limit == nil || *query.Limit != 1) {
		return errors.New("senergy_db_v3.client - an aggregated device group needs a groupTime, or a limit of 1 for the latest value")
	}
	return nil
}

// AggregateDeviceGroup combines the first series of every response element into
// one series of [time, value] rows, taking column 1 of each row as the device's
// value. With a groupTime rows are matched by timestamp, otherwise (limit 1) by position.
func AggregateDeviceGroup(elements []timescaleModels.QueriesV2ResponseElement, query timescaleModels.QueriesRequestElement, aggregation string) ([][]interface{}, error) {
	if err := validateAggregate(query, aggregation); err != nil {
		return nil, err
	}
	byTimestamp := query.GroupTime != nil
	var rows []*groupRow
	index := map[[2]int64]int{}
	for _, element := range elements {
		if len(element.Data) == 0 {
			continue
		}
		for i, row := range element.Data[0] {
			if len(row) < 2 {
				return nil, errors.New("senergy_db_v3.client - response row of " + deviceLabel(element) + " has no value column")
			}
			var target *groupRow
			if byTimestamp {
				instant, err := rowInstant(row[0])
				if err != nil {
					return nil, errors.New("senergy_db_v3.client - " + deviceLabel(element) + ": " + err.Error())
				}
				// UnixNano would overflow outside of 1678..2262, seconds plus nanoseconds do not.
				key := [2]int64{instant.Unix(), int64(instant.Nanosecond())}
				pos, ok := index[key]
				if !ok {
					pos = len(rows)
					index[key] = pos
					rows = append(rows, &groupRow{timestamp: row[0], instant: instant})
				}
				target = rows[pos]
			} else {
				if i == len(rows) {
					rows = append(rows, &groupRow{timestamp: row[0]})
				}
				target = rows[i]
			}
			switch value := row[1].(type) {
			case nil:
			case float64:
				target.sum += value
				target.scaledSum += math.Ldexp(value, -meanScale)
				target.count++
			default:
				return nil, fmt.Errorf("senergy_db_v3.client - %s returned a non-numeric value of type %T", deviceLabel(element), row[1])
			}
		}
	}
	if byTimestamp {
		// Devices report different sets of timestamps, so the merged rows are sorted
		// again, in the direction the wrapper sorts each series (ascending by default).
		descending := query.OrderDirection != nil && *query.OrderDirection == timescaleModels.Desc
		sort.SliceStable(rows, func(i, j int) bool {
			if descending {
				return rows[i].instant.After(rows[j].instant)
			}
			return rows[i].instant.Before(rows[j].instant)
		})
		// The union of all devices' timestamps can exceed the limit each series obeys.
		if query.Limit != nil && *query.Limit >= 0 && len(rows) > *query.Limit {
			rows = rows[:*query.Limit]
		}
	}
	result := make([][]interface{}, 0, len(rows))
	for _, row := range rows {
		var value interface{}
		if row.count > 0 {
			combined := row.sum
			if aggregation == lib.AggregationMean {
				combined = row.sum / float64(row.count)
				if math.IsInf(row.sum, 0) {
					combined = math.Ldexp(row.scaledSum/float64(row.count), meanScale)
				}
			}
			if math.IsInf(combined, 0) || math.IsNaN(combined) {
				return nil, fmt.Errorf("senergy_db_v3.client - aggregated value at %v is out of range", row.timestamp)
			}
			value = combined
		}
		result = append(result, []interface{}{row.timestamp, value})
	}
	return result, nil
}

func rowInstant(timestamp interface{}) (time.Time, error) {
	s, ok := timestamp.(string)
	if !ok {
		return time.Time{}, fmt.Errorf("timestamp of type %T cannot be aligned", timestamp)
	}
	instant, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("timestamp %q is not RFC 3339", s)
	}
	return instant, nil
}

func deviceLabel(element timescaleModels.QueriesV2ResponseElement) string {
	label := "device <unknown>"
	if element.DeviceId != nil {
		label = "device " + *element.DeviceId
	}
	if element.ServiceId != nil {
		label += " (service " + *element.ServiceId + ")"
	}
	return label
}
