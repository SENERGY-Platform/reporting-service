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

import "fmt"

// FiguresQuery asks the figures service for the consumption of a unit graph over a
// calendar period. The resolved answer replaces the report object in the data
// handed to the template, in the form of FiguresResult.
type FiguresQuery struct {
	GraphId string `json:"graphId"`
	// NodeId narrows the rows to this node and everything below it.
	NodeId *string `json:"nodeId,omitempty"`
	// Carriers narrows the figures, default every carrier that occurs.
	Carriers []string      `json:"carriers,omitempty"`
	Period   FiguresPeriod `json:"period"`
	// Interval cuts the period into calendar steps (day, week, month, quarter or
	// year) and adds the steps and a series per row to the result. Absent: totals only.
	Interval *string `json:"interval,omitempty" enums:"day,week,month,quarter,year"`
}

const (
	IntervalDay     = "day"
	IntervalWeek    = "week"
	IntervalMonth   = "month"
	IntervalQuarter = "quarter"
	IntervalYear    = "year"
)

// ValidateInterval accepts an absent interval and the five known ones.
func ValidateInterval(interval *string) error {
	if interval == nil {
		return nil
	}
	switch *interval {
	case IntervalDay, IntervalWeek, IntervalMonth, IntervalQuarter, IntervalYear:
		return nil
	}
	return fmt.Errorf("unknown interval %q, use day, week, month, quarter or year", *interval)
}

// ValidateFiguresIntervals checks the interval of every figuresQuery in report data,
// so a bad one fails at the API edge and not in a job.
func ValidateFiguresIntervals(data map[string]ReportObject) error {
	for key, object := range data {
		if object.FiguresQuery != nil {
			if err := ValidateInterval(object.FiguresQuery.Interval); err != nil {
				return fmt.Errorf("report object %s: %w", key, err)
			}
		}
		if err := ValidateFiguresIntervals(object.Fields); err != nil {
			return err
		}
		if err := ValidateFiguresIntervals(object.Children); err != nil {
			return err
		}
	}
	return nil
}

// FiguresPeriod is a calendar period, resolved against the time of the run.
type FiguresPeriod struct {
	// Kind is previousDay, previousWeek (ISO, Monday first), previousMonth,
	// previousQuarter, previousYear, currentMonth, currentYear or fixed.
	Kind string `json:"kind"`
	// Start and End are days as YYYY-MM-DD, the end day included. Only for fixed.
	Start *string `json:"start,omitempty"`
	End   *string `json:"end,omitempty"`
	// TimeZone is an IANA zone, default Europe/Berlin.
	TimeZone *string `json:"timeZone,omitempty"`
}

const (
	PeriodPreviousDay     = "previousDay"
	PeriodPreviousWeek    = "previousWeek"
	PeriodPreviousMonth   = "previousMonth"
	PeriodPreviousQuarter = "previousQuarter"
	PeriodPreviousYear    = "previousYear"
	PeriodCurrentMonth    = "currentMonth"
	PeriodCurrentYear     = "currentYear"
	PeriodFixed           = "fixed"

	DefaultTimeZone = "Europe/Berlin"
)

// Meter data coverage as classified for a figures result.
const (
	CoverageNone     = "none"
	CoveragePartial  = "partial"
	CoverageComplete = "complete"
)

// FiguresResult is what a report object with a figuresQuery turns into.
type FiguresResult struct {
	Graph  FiguresRef     `json:"graph"`
	Node   *FiguresRef    `json:"node"`
	Period ResolvedPeriod `json:"period"`
	// Carriers are the requested ones, or the ones that occur in rows.
	Carriers []string       `json:"carriers"`
	Rows     []FiguresRow   `json:"rows"`
	Meters   []FiguresMeter `json:"meters"`
	// NotMeters are device nodes without a reading whose device type is no meter
	// (storage, plant without a register), which are no gap in the data.
	NotMeters []FiguresNotMeter `json:"notMeters"`
	// Generation is the output of each plant, one entry per device and carrier.
	Generation []FiguresPlant `json:"generation"`
	// GeneratedAt is an RFC 3339 instant.
	GeneratedAt string `json:"generatedAt"`
	// Steps are the calendar steps of the period in order, only with an interval.
	// Every row's Series has one entry per step.
	Steps []FiguresStep `json:"steps,omitempty"`
}

// FiguresStep is one step of an interval. Start and End are days as YYYY-MM-DD in
// the report's zone with the end day included, Label is German, such as
// "KW 40 2026", "Oktober 2026" or "Q4 2026".
type FiguresStep struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Label string `json:"label"`
}

type FiguresRef struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

// ResolvedPeriod holds calendar days with the end day included.
type ResolvedPeriod struct {
	Kind     string `json:"kind"`
	Start    string `json:"start"`
	End      string `json:"end"`
	TimeZone string `json:"timeZone"`
}

// FiguresRow is one node of the tree. A carrier the node never measured is absent
// from Values.
type FiguresRow struct {
	NodeId string `json:"nodeId"`
	Name   string `json:"name"`
	// Path holds the node names from the top node down to this node.
	Path   []string            `json:"path"`
	Depth  int                 `json:"depth"`
	Values map[string]*float64 `json:"values"`
	// Share is the weight of the edge to the parent the row stands under, in
	// percent, and null for a top row.
	Share *float64 `json:"share"`
	// AlsoUnder names the other parents the node hangs under, sorted.
	AlsoUnder []string `json:"alsoUnder"`
	// Generation is the kWh per carrier that the plants at and below this node made,
	// not weighted by shares, and absent where there are none.
	Generation map[string]float64 `json:"generation,omitempty"`
	// Series is the values of the node per step, in the order of the result's Steps,
	// only with an interval. A step with nothing for the node is an empty object.
	// The steps do not add up to Values exactly: each is read from its first to its
	// last reading.
	Series []map[string]*float64 `json:"series,omitempty"`
}

// FiguresMeter is the data coverage of one device node. First and Last are the days
// of the first and last reading and absent for status none.
type FiguresMeter struct {
	NodeId   string `json:"nodeId"`
	DeviceId string `json:"deviceId"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	First    string `json:"first,omitempty"`
	Last     string `json:"last,omitempty"`
}

// FiguresNotMeter is a device node that is not a meter: it has no reading and its
// device type does not name a meter.
type FiguresNotMeter struct {
	NodeId     string `json:"nodeId"`
	DeviceId   string `json:"deviceId"`
	Name       string `json:"name"`
	DeviceType string `json:"deviceType"`
}

// FiguresPlant is what one plant made over the period. A device with a feed-in
// register and no draw register for the carrier is a plant, a grid meter with both
// is not and its feed-in is not listed.
type FiguresPlant struct {
	NodeId   string  `json:"nodeId"`
	DeviceId string  `json:"deviceId"`
	Name     string  `json:"name"`
	Carrier  string  `json:"carrier"`
	Kwh      float64 `json:"kwh"`
}
