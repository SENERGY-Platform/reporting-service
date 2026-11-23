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
	// GeneratedAt is an RFC 3339 instant.
	GeneratedAt string `json:"generatedAt"`
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
