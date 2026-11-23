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
	"strings"
	"testing"
)

func TestValidateInterval(t *testing.T) {
	for _, ok := range []string{"day", "week", "month", "quarter", "year"} {
		if err := ValidateInterval(&ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	if err := ValidateInterval(nil); err != nil {
		t.Errorf("an absent interval rejected: %v", err)
	}
	for _, bad := range []string{"", "hour", "Day", "weeks", " week"} {
		err := ValidateInterval(&bad)
		if err == nil || !strings.Contains(err.Error(), "day, week, month, quarter or year") {
			t.Errorf("%q: error = %v, want one naming the five", bad, err)
		}
	}
}

func TestValidateFiguresIntervalsFindsAQueryInsideFieldsAndChildren(t *testing.T) {
	bad := "hour"
	nested := map[string]ReportObject{
		"outer": {Children: map[string]ReportObject{
			"inner": {Fields: map[string]ReportObject{
				"consumption": {FiguresQuery: &FiguresQuery{GraphId: "g", Interval: &bad}},
			}},
		}},
	}
	err := ValidateFiguresIntervals(nested)
	if err == nil || !strings.Contains(err.Error(), "consumption") || !strings.Contains(err.Error(), `"hour"`) {
		t.Errorf("error = %v, want one naming the object and the value", err)
	}
	if err := ValidateFiguresIntervals(map[string]ReportObject{"a": {FiguresQuery: &FiguresQuery{GraphId: "g"}}}); err != nil {
		t.Errorf("a query without interval rejected: %v", err)
	}
}
