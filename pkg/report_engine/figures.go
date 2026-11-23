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
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"
	// The runtime image has no zone database, and periods are cut in the report's zone.
	_ "time/tzdata"

	"github.com/SENERGY-Platform/reporting-service/lib"
	"github.com/SENERGY-Platform/reporting-service/pkg/apis/device_repository"
	"github.com/SENERGY-Platform/reporting-service/pkg/apis/figures"
	"github.com/SENERGY-Platform/reporting-service/pkg/config"
	"github.com/SENERGY-Platform/reporting-service/pkg/util"
)

const (
	dayLayout = "2006-01-02"
	day       = 24 * time.Hour

	defaultFiguresTimeout = 2 * time.Minute
	maxGraphIdBytes       = 256
	maxErrorBodyRunes     = 300
	excludedKey           = "dashboard/excluded"
)

// ParseFiguresTimeout reads FIGURES_TIMEOUT, which has to be a positive duration.
func ParseFiguresTimeout(cfg *config.Config) (time.Duration, error) {
	timeout, err := time.ParseDuration(cfg.Figures.Timeout)
	if err != nil {
		return 0, errors.New("invalid figures timeout: " + err.Error())
	}
	if timeout <= 0 {
		return 0, errors.New("figures timeout has to be positive")
	}
	return timeout, nil
}

// FiguresTimeout is the configured timeout, or the default where it is unusable.
func FiguresTimeout(cfg *config.Config) time.Duration {
	if timeout, err := ParseFiguresTimeout(cfg); err == nil {
		return timeout
	}
	return defaultFiguresTimeout
}

// validateGraphId applies the rule figures applies, so a bad id fails before any call.
func validateGraphId(id string) error {
	switch {
	case id == "":
		return errors.New("figuresQuery needs a graphId")
	case id == "." || id == "..":
		return errors.New("graphId must not be a dot segment")
	case strings.ContainsAny(id, "/?#"):
		return errors.New("graphId must not contain a slash, a question mark or a number sign")
	case strings.IndexFunc(id, unicode.IsSpace) >= 0:
		return errors.New("graphId must not contain whitespace")
	case len(id) > maxGraphIdBytes:
		return fmt.Errorf("graphId must not be longer than %d bytes", maxGraphIdBytes)
	}
	return nil
}

// carrierOrder is the order carriers are listed in, others follow alphabetically.
var carrierOrder = []string{"electricity", "gas", "biogas", "oil", "heat", "water"}

// periodWindow is a half-open window cut at midnight of its zone.
type periodWindow struct {
	kind     string
	location *time.Location
	start    time.Time
	end      time.Time
}

// resolvePeriod resolves a calendar period against now. The window ends at the
// start of a day, and current periods end at the start of the day now falls on.
func resolvePeriod(period lib.FiguresPeriod, now time.Time) (periodWindow, error) {
	zone := lib.DefaultTimeZone
	if period.TimeZone != nil {
		zone = *period.TimeZone
	}
	// LoadLocation answers "" with UTC and "Local" with the zone of the server.
	if zone == "" || zone == "Local" {
		return periodWindow{}, fmt.Errorf("unknown time zone %q", zone)
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return periodWindow{}, fmt.Errorf("unknown time zone %q", zone)
	}
	local := now.In(loc)
	y, m, d := local.Date()
	at := func(year int, month time.Month, dayOfMonth int) time.Time {
		return startOfDay(loc, year, month, dayOfMonth)
	}
	today := at(y, m, d)
	window := periodWindow{kind: period.Kind, location: loc}
	switch period.Kind {
	case lib.PeriodPreviousDay:
		window.start, window.end = at(y, m, d-1), today
	case lib.PeriodPreviousWeek:
		monday := at(y, m, d-(int(local.Weekday())+6)%7)
		my, mm, md := monday.Date()
		window.start, window.end = at(my, mm, md-7), monday
	case lib.PeriodPreviousMonth:
		window.start, window.end = at(y, m-1, 1), at(y, m, 1)
	case lib.PeriodPreviousQuarter:
		quarter := time.Month((int(m)-1)/3*3 + 1)
		window.start, window.end = at(y, quarter-3, 1), at(y, quarter, 1)
	case lib.PeriodPreviousYear:
		window.start, window.end = at(y-1, 1, 1), at(y, 1, 1)
	case lib.PeriodCurrentMonth:
		window.start, window.end = at(y, m, 1), today
	case lib.PeriodCurrentYear:
		window.start, window.end = at(y, 1, 1), today
	case lib.PeriodFixed:
		if period.Start == nil || period.End == nil {
			return periodWindow{}, errors.New("period fixed needs start and end")
		}
		start, err := time.Parse(dayLayout, *period.Start)
		if err != nil {
			return periodWindow{}, fmt.Errorf("period start %q is not a day as YYYY-MM-DD", *period.Start)
		}
		end, err := time.Parse(dayLayout, *period.End)
		if err != nil {
			return periodWindow{}, fmt.Errorf("period end %q is not a day as YYYY-MM-DD", *period.End)
		}
		if end.Before(start) {
			return periodWindow{}, fmt.Errorf("period end %s is before its start %s", *period.End, *period.Start)
		}
		sy, sm, sd := start.Date()
		ey, em, ed := end.Date()
		window.start, window.end = at(sy, sm, sd), at(ey, em, ed+1)
	case "":
		return periodWindow{}, errors.New("period needs a kind")
	default:
		return periodWindow{}, fmt.Errorf("unknown period kind %q", period.Kind)
	}
	if !window.end.After(window.start) {
		return periodWindow{}, fmt.Errorf("period %s has no completed day on %s", period.Kind, today.Format(dayLayout))
	}
	return window, nil
}

// startOfDay is the first instant of a calendar day in loc. Where a zone skips
// midnight, as Santiago does on the day summer time starts, that is not 00:00.
func startOfDay(loc *time.Location, year int, month time.Month, dayOfMonth int) time.Time {
	wy, wm, wd := time.Date(year, month, dayOfMonth, 0, 0, 0, 0, time.UTC).Date()
	want := wy*10000 + int(wm)*100 + wd
	key := func(t time.Time) int {
		y, m, d := t.In(loc).Date()
		return y*10000 + int(m)*100 + d
	}
	t := time.Date(wy, wm, wd, 0, 0, 0, 0, loc)
	for i := 0; i < 2*24*60 && key(t) > want; i++ {
		t = t.Add(-time.Minute)
	}
	for i := 0; i < 2*24*60 && key(t.Add(-time.Minute)) == want; i++ {
		t = t.Add(-time.Minute)
	}
	for i := 0; i < 2*24*60 && key(t) < want; i++ {
		t = t.Add(time.Minute)
	}
	return t
}

// resolved describes the window as calendar days with the end day included.
func (w periodWindow) resolved() lib.ResolvedPeriod {
	y, m, d := w.end.Date()
	return lib.ResolvedPeriod{
		Kind:  w.kind,
		Start: w.start.Format(dayLayout),
		// Noon exists in every zone, midnight may not.
		End:      time.Date(y, m, d-1, 12, 0, 0, 0, w.location).Format(dayLayout),
		TimeZone: w.location.String(),
	}
}

func (r *Client) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// figuresData resolves a figuresQuery: the graph from the device repository, the
// totals from figures, both with the token of the run.
func (r *Client) figuresData(authToken string, query lib.FiguresQuery) (lib.FiguresResult, error) {
	if r.Figures == nil || r.DeviceRepository == nil {
		return lib.FiguresResult{}, errors.New("figuresQuery needs FIGURES_URL to be configured")
	}
	if err := validateGraphId(query.GraphId); err != nil {
		return lib.FiguresResult{}, err
	}
	carriers, err := normalizeCarriers(query.Carriers)
	if err != nil {
		return lib.FiguresResult{}, err
	}
	if err = lib.ValidateInterval(query.Interval); err != nil {
		return lib.FiguresResult{}, err
	}
	now := r.clock()
	window, err := resolvePeriod(query.Period, now)
	if err != nil {
		return lib.FiguresResult{}, err
	}
	graph, err := r.DeviceRepository.GetGraph(authToken, query.GraphId)
	if err != nil {
		return lib.FiguresResult{}, loggedPlatformError("reading the graph from the device repository", query.GraphId, err)
	}
	// Asked for the whole graph even with a node: figures answers one node's total
	// only, while the rows hold every node below it.
	request := figures.TotalsRequest{
		GraphId:  query.GraphId,
		Range:    figures.Range{Start: window.start.Format(time.RFC3339), End: window.end.Format(time.RFC3339)},
		TimeZone: window.location.String(),
		Routing:  "edges",
		Net:      true,
	}
	if query.Interval != nil {
		request.Interval = *query.Interval
	}
	totals, err := r.Figures.Totals(authToken, request)
	if err != nil {
		if rejected := figuresRejection(err); rejected != nil {
			util.Logger.Warn("figures rejected the query", "graph_id", query.GraphId, "error", rejected.Error())
			return lib.FiguresResult{}, rejected
		}
		return lib.FiguresResult{}, loggedPlatformError("reading the consumption from figures", query.GraphId, err)
	}
	return buildFiguresResult(graph, totals, query, carriers, window, now, r.deviceTypes(authToken, graph, totals))
}

// deviceTypes reads the type names of the device nodes that have no reading, the
// only ones that can turn out not to be meters. Where the repository does not
// answer, the types stay unknown and those devices count as meters.
func (r *Client) deviceTypes(authToken string, graph device_repository.Graph, totals figures.TotalsResponse) map[string]string {
	var ids []string
	seen := map[string]bool{}
	for _, node := range graph.Nodes {
		id := node.ResourceId
		if node.ResourceType != "device" || id == "" || seen[id] || excluded(node) || len(totals.ReadingsByDevice[id]) > 0 {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	types, err := r.DeviceRepository.GetDeviceTypeNames(authToken, ids)
	if err != nil {
		util.Logger.Warn("reading device types failed, devices without readings count as meters", "error", platformError("reading the device types", "", err).Error())
		return nil
	}
	return types
}

// FiguresRejectedError is a 400 of figures, such as too many steps for an interval.
// The message is figures' own sentence, which names what to change.
type FiguresRejectedError struct {
	Message string
}

func (e *FiguresRejectedError) Error() string {
	return "figures rejected the query: " + e.Message
}

// figuresRejection returns the 400 of figures as an error the API can pass on, and
// nil for any other failure.
func figuresRejection(err error) *FiguresRejectedError {
	var statusErr *figures.StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != http.StatusBadRequest {
		return nil
	}
	message := statusErr.Body
	var body struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(statusErr.Body), &body) == nil {
		if body.Error != "" {
			message = body.Error
		} else if body.Message != "" {
			message = body.Message
		}
	}
	return &FiguresRejectedError{Message: oneLine(message)}
}

func loggedPlatformError(doing string, graphId string, err error) error {
	wrapped := platformError(doing, graphId, err)
	util.Logger.Warn("figures query failed", "graph_id", graphId, "error", wrapped.Error())
	return wrapped
}

// platformError turns a refusal of a platform service into a message a report
// owner can act on, followed by the status and the start of the answer.
func platformError(doing string, graphId string, err error) error {
	var figuresErr *figures.StatusError
	var repositoryErr *device_repository.StatusError
	status, body, fromFigures := 0, "", false
	switch {
	case errors.As(err, &figuresErr):
		status, body, fromFigures = figuresErr.Status, figuresErr.Body, true
	case errors.As(err, &repositoryErr):
		status, body = repositoryErr.Status, repositoryErr.Body
	default:
		return fmt.Errorf("%s failed: %w", doing, err)
	}
	var what string
	switch {
	case status == http.StatusUnauthorized:
		what = "the token of the run is not accepted"
	case status == http.StatusForbidden:
		what = fmt.Sprintf("the report owner may not read graph %s or its devices", graphId)
	case status == http.StatusNotFound && fromFigures:
		// The graph was read just before, so the graph is not what is missing.
		what = "figures was not found under the configured FIGURES_URL"
	case status == http.StatusNotFound:
		what = fmt.Sprintf("graph %s does not exist", graphId)
	default:
		what = "the service answered with an error"
	}
	return fmt.Errorf("%s failed: %s (status %d: %s)", doing, what, status, oneLine(body))
}

// oneLine puts an answer on one line and cuts it to a length fit for a message.
func oneLine(text string) string {
	line := strings.Join(strings.Fields(text), " ")
	if runes := []rune(line); len(runes) > maxErrorBodyRunes {
		return string(runes[:maxErrorBodyRunes]) + "..."
	}
	return line
}

// normalizeCarriers validates the requested carriers and puts them in carrier order.
func normalizeCarriers(requested []string) ([]string, error) {
	seen := map[string]bool{}
	for _, carrier := range requested {
		if !contains(carrierOrder, carrier) {
			return nil, fmt.Errorf("unknown carrier %q", carrier)
		}
		seen[carrier] = true
	}
	var carriers []string
	for _, carrier := range carrierOrder {
		if seen[carrier] {
			carriers = append(carriers, carrier)
		}
	}
	return carriers, nil
}

func contains(list []string, item string) bool {
	for _, entry := range list {
		if entry == item {
			return true
		}
	}
	return false
}

func attribute(attributes []device_repository.Attribute, key string) string {
	for _, attr := range attributes {
		if attr.Key == key {
			return attr.Value
		}
	}
	return ""
}

func nodeName(node device_repository.GraphNode) string {
	if name := attribute(node.Attributes, "name"); name != "" {
		return name
	}
	return node.Id
}

// graphName is the graph's name attribute, else the name of the first named node
// that is no edge's child, else its id.
func graphName(graph device_repository.Graph) string {
	if name := attribute(graph.Attributes, "name"); name != "" {
		return name
	}
	hasParent := map[string]bool{}
	for _, edge := range graph.Edges {
		hasParent[edge.FromNodeId] = true
	}
	for _, node := range graph.Nodes {
		if hasParent[node.Id] {
			continue
		}
		if name := attribute(node.Attributes, "name"); name != "" {
			return name
		}
	}
	return graph.Id
}

// live reports whether an edge takes part: weight zero means detached, an absent
// weight stays in, as it does in figures, with a share that is not known.
func live(edge device_repository.GraphEdge) bool {
	return edge.Weight == nil || *edge.Weight > 0
}

func excluded(node device_repository.GraphNode) bool {
	return attribute(node.Attributes, excludedKey) == "true"
}

// clampedShare is the edge weight in percent as figures reads it, between 0 and 100.
func clampedShare(weight *float64) *float64 {
	if weight == nil {
		return nil
	}
	share := math.Min(math.Max(*weight, 0), 100)
	return &share
}

// buildFiguresResult maps a graph and the totals asked for it to the result form.
// A node with several parents appears once, under the first parent reached, so sums
// over rows do not count it twice, and names the others in AlsoUnder. A node marked
// as excluded gets no row and no meter, the nodes below it are still listed.
// A device without a reading whose type is known and no meter is a not-meter, and
// the plants among the devices are listed with what they made.
func buildFiguresResult(graph device_repository.Graph, totals figures.TotalsResponse, query lib.FiguresQuery, carriers []string, window periodWindow, now time.Time, deviceTypes map[string]string) (lib.FiguresResult, error) {
	byId := make(map[string]device_repository.GraphNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		byId[node.Id] = node
	}
	type parentLink struct {
		parent string
		weight *float64
	}
	children := map[string][]string{}
	parents := map[string][]parentLink{}
	for _, edge := range graph.Edges {
		_, childKnown := byId[edge.FromNodeId]
		_, parentKnown := byId[edge.ToNodeId]
		if !live(edge) || !childKnown || !parentKnown {
			continue
		}
		children[edge.ToNodeId] = append(children[edge.ToNodeId], edge.FromNodeId)
		parents[edge.FromNodeId] = append(parents[edge.FromNodeId], parentLink{edge.ToNodeId, edge.Weight})
	}
	less := func(a, b string) bool {
		na, nb := strings.ToLower(nodeName(byId[a])), strings.ToLower(nodeName(byId[b]))
		if na != nb {
			return na < nb
		}
		return a < b
	}
	for parent := range children {
		sort.Slice(children[parent], func(i, j int) bool { return less(children[parent][i], children[parent][j]) })
	}

	var steps []lib.FiguresStep
	if query.Interval != nil {
		var err error
		if steps, err = buildSteps(totals.Series, *query.Interval, window.location); err != nil {
			return lib.FiguresResult{}, err
		}
	}

	result := lib.FiguresResult{
		Graph:       lib.FiguresRef{Id: query.GraphId, Name: graphName(graph)},
		Period:      window.resolved(),
		Rows:        []lib.FiguresRow{},
		Meters:      []lib.FiguresMeter{},
		NotMeters:   []lib.FiguresNotMeter{},
		Generation:  []lib.FiguresPlant{},
		GeneratedAt: now.In(window.location).Format(time.RFC3339),
		Steps:       steps,
	}
	var roots []string
	if query.NodeId != nil {
		node, ok := byId[*query.NodeId]
		if !ok {
			return result, fmt.Errorf("node %s is not part of graph %s", *query.NodeId, query.GraphId)
		}
		result.Node = &lib.FiguresRef{Id: node.Id, Name: nodeName(node)}
		roots = []string{node.Id}
	} else {
		for _, node := range graph.Nodes {
			if len(parents[node.Id]) == 0 {
				roots = append(roots, node.Id)
			}
		}
		sort.Slice(roots, func(i, j int) bool { return less(roots[i], roots[j]) })
	}

	visited := map[string]bool{}
	plantSeen := map[string]bool{}
	// walk returns what the plants at and below the node made, per carrier.
	var walk func(id string, under string, path []string) map[string]float64
	walk = func(id string, under string, path []string) map[string]float64 {
		if visited[id] {
			return nil
		}
		visited[id] = true
		node := byId[id]
		made := map[string]float64{}
		rowIndex := -1
		if !excluded(node) {
			path = append(path[:len(path):len(path)], nodeName(node))
			row := lib.FiguresRow{
				NodeId:    id,
				Name:      nodeName(node),
				Path:      path,
				Depth:     len(path) - 1,
				Values:    rowValues(totals.Totals[id], carriers),
				AlsoUnder: []string{},
			}
			if steps != nil {
				row.Series = rowSeries(totals.Series, id, carriers)
			}
			if under != "" {
				for _, link := range parents[id] {
					if link.parent == under {
						if row.Share == nil {
							row.Share = clampedShare(link.weight)
						}
						continue
					}
					row.AlsoUnder = append(row.AlsoUnder, nodeName(byId[link.parent]))
				}
				sort.Strings(row.AlsoUnder)
			}
			result.Rows = append(result.Rows, row)
			rowIndex = len(result.Rows) - 1
			if node.ResourceType == "device" && node.ResourceId != "" {
				meter := meterCoverage(node, totals.ReadingsByDevice[node.ResourceId], window)
				if typeName := deviceTypes[node.ResourceId]; meter.Status == lib.CoverageNone && typeName != "" && !isMeterType(typeName) {
					result.NotMeters = append(result.NotMeters, lib.FiguresNotMeter{NodeId: id, DeviceId: node.ResourceId, Name: nodeName(node), DeviceType: typeName})
				} else {
					result.Meters = append(result.Meters, meter)
				}
				for _, plant := range plantsOf(node, totals, carriers) {
					key := plant.DeviceId + "/" + plant.Carrier
					if plantSeen[key] {
						continue
					}
					plantSeen[key] = true
					result.Generation = append(result.Generation, plant)
					made[plant.Carrier] += plant.Kwh
				}
			}
		}
		for _, child := range children[id] {
			for carrier, kwh := range walk(child, id, path) {
				made[carrier] += kwh
			}
		}
		if rowIndex >= 0 && len(made) > 0 {
			result.Rows[rowIndex].Generation = made
		}
		return made
	}
	for _, root := range roots {
		walk(root, "", nil)
	}

	result.Carriers = carriers
	if result.Carriers == nil {
		result.Carriers = occurringCarriers(result.Rows)
	}
	return result, nil
}

// rowValues keeps the carriers asked for, or all of them. A carrier the node never
// measured stays out of the map.
func rowValues(values map[string]*float64, carriers []string) map[string]*float64 {
	row := map[string]*float64{}
	for carrier, value := range values {
		if carriers == nil || contains(carriers, carrier) {
			row[carrier] = value
		}
	}
	return row
}

// rowSeries is the values of a node in every step, cut to the carriers like the row's
// totals. A step that holds nothing for the node gives an empty object.
func rowSeries(series []figures.Step, nodeId string, carriers []string) []map[string]*float64 {
	out := make([]map[string]*float64, len(series))
	for i, step := range series {
		out[i] = rowValues(step.Totals[nodeId], carriers)
	}
	return out
}

var germanMonths = []string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"}

// buildSteps turns the steps of figures into days of the zone with a German label.
// Figures cuts at midnight and ends a step at the start of the next one, so the end
// day is the day before the end instant.
func buildSteps(series []figures.Step, interval string, loc *time.Location) ([]lib.FiguresStep, error) {
	if len(series) == 0 {
		return nil, errors.New("figures answered without series for the interval")
	}
	steps := make([]lib.FiguresStep, 0, len(series))
	for _, step := range series {
		start, err := time.Parse(time.RFC3339, step.Start)
		if err != nil {
			return nil, fmt.Errorf("figures answered a step start %q that is no instant", step.Start)
		}
		end, err := time.Parse(time.RFC3339, step.End)
		if err != nil {
			return nil, fmt.Errorf("figures answered a step end %q that is no instant", step.End)
		}
		sy, sm, sd := start.In(loc).Date()
		ey, em, ed := end.In(loc).Date()
		steps = append(steps, lib.FiguresStep{
			Start: time.Date(sy, sm, sd, 12, 0, 0, 0, loc).Format(dayLayout),
			// Noon exists in every zone, midnight may not.
			End:   time.Date(ey, em, ed-1, 12, 0, 0, 0, loc).Format(dayLayout),
			Label: stepLabel(interval, time.Date(sy, sm, sd, 12, 0, 0, 0, loc)),
		})
	}
	return steps, nil
}

// stepLabel names the step that starts on the given day. A week is named after its
// ISO year, which differs from the calendar year around New Year.
func stepLabel(interval string, start time.Time) string {
	switch interval {
	case lib.IntervalDay:
		return start.Format("02.01.2006")
	case lib.IntervalWeek:
		year, week := start.ISOWeek()
		return fmt.Sprintf("KW %d %d", week, year)
	case lib.IntervalMonth:
		return fmt.Sprintf("%s %d", germanMonths[start.Month()-1], start.Year())
	case lib.IntervalQuarter:
		return fmt.Sprintf("Q%d %d", (int(start.Month())-1)/3+1, start.Year())
	default:
		return fmt.Sprintf("%d", start.Year())
	}
}

func occurringCarriers(rows []lib.FiguresRow) []string {
	seen := map[string]bool{}
	for _, row := range rows {
		for carrier := range row.Values {
			seen[carrier] = true
		}
	}
	carriers := []string{}
	for _, carrier := range carrierOrder {
		if seen[carrier] {
			carriers = append(carriers, carrier)
			delete(seen, carrier)
		}
	}
	var others []string
	for carrier := range seen {
		others = append(others, carrier)
	}
	sort.Strings(others)
	return append(carriers, others...)
}

// meterCoverage classifies a meter from the two ends of its readings: partial when
// the first reading is more than a day after the start or the last more than a day
// before the end. A register may lack an end, as a feed-in register may lack its
// first, and does not hide the ends of the others. Gaps between the two ends are
// not visible here.
func meterCoverage(node device_repository.GraphNode, readings []figures.MeterReading, window periodWindow) lib.FiguresMeter {
	meter := lib.FiguresMeter{NodeId: node.Id, DeviceId: node.ResourceId, Name: nodeName(node), Status: lib.CoverageNone}
	var first, last time.Time
	for _, reading := range readings {
		if reading.First != nil {
			if at, err := time.Parse(time.RFC3339, reading.First.At); err == nil && (first.IsZero() || at.Before(first)) {
				first = at
			}
		}
		if reading.Last != nil {
			if at, err := time.Parse(time.RFC3339, reading.Last.At); err == nil && (last.IsZero() || at.After(last)) {
				last = at
			}
		}
	}
	if first.IsZero() && last.IsZero() {
		return meter
	}
	meter.Status = lib.CoverageComplete
	if (!first.IsZero() && first.Sub(window.start) > day) || (!last.IsZero() && window.end.Sub(last) > day) {
		meter.Status = lib.CoveragePartial
	}
	if !first.IsZero() {
		meter.First = first.In(window.location).Format(dayLayout)
	}
	if !last.IsZero() {
		meter.Last = last.In(window.location).Format(dayLayout)
	}
	return meter
}

// isMeterType is the approximation dashboard-mcp uses: a device type whose name has
// no "meter" or "zähler" in it (storage, CHP unit, inverter) is not a meter.
func isMeterType(typeName string) bool {
	name := strings.ToLower(typeName)
	return strings.Contains(name, "meter") || strings.Contains(name, "zähler") || strings.Contains(name, "zaehler")
}

// plantsOf lists what a device made per energy carrier. A device with a feed-in
// register and no draw register for the carrier is a plant, one with both is a grid
// meter whose feed-in is no generation.
func plantsOf(node device_repository.GraphNode, totals figures.TotalsResponse, carriers []string) []lib.FiguresPlant {
	var plants []lib.FiguresPlant
	for _, carrier := range carrierOrder {
		made := totals.GenerationByDevice[node.ResourceId][carrier]
		if carrier == "water" || made <= 0 || math.IsInf(made, 0) || math.IsNaN(made) || (carriers != nil && !contains(carriers, carrier)) {
			continue
		}
		drawn := false
		for _, reading := range totals.ReadingsByDevice[node.ResourceId] {
			if reading.Carrier == carrier && (reading.Direction == "" || reading.Direction == "draw") {
				drawn = true
			}
		}
		if !drawn {
			plants = append(plants, lib.FiguresPlant{NodeId: node.Id, DeviceId: node.ResourceId, Name: nodeName(node), Carrier: carrier, Kwh: made})
		}
	}
	return plants
}
