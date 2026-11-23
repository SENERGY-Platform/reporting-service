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
	"fmt"
	"regexp"
	"strings"
)

const (
	fixedHelpersBegin = "// reporting-service fixed helpers: begin"
	fixedHelpersEnd   = "// reporting-service fixed helpers: end"
)

// FixedTemplateHelpers are the formatting helpers every template written through
// POST or PUT /templates carries in front of its author's helpers. Only function
// declarations become handlebars helpers in jsreport, so the lookup tables are consts.
const FixedTemplateHelpers = fixedHelpersBegin + `
const FIXED_CARRIERS = {electricity: 'Strom', gas: 'Gas', biogas: 'Biogas', oil: 'Heizöl', heat: 'Wärme', water: 'Wasser'}
const FIXED_STATUSES = {none: 'ohne Daten', partial: 'Teilzeitraum', complete: 'vollständig'}

// German number format with thousands dots, empty for a missing or non-numeric value.
function number(value, decimals) {
    if (typeof value === 'string' && value.trim() !== '') value = Number(value)
    if (typeof value !== 'number' || !isFinite(value)) return ''
    const places = typeof decimals === 'number' && decimals > 0 ? Math.min(Math.floor(decimals), 10) : 0
    const text = new Intl.NumberFormat('de-DE', {minimumFractionDigits: places, maximumFractionDigits: places}).format(value)
    return text.replace(/^-(0(,0+)?)$/, '$1')
}

// A day as YYYY-MM-DD becomes DD.MM.YYYY, reordered as text so no time zone shifts it.
function date(yyyyMmDd) {
    const match = typeof yyyyMmDd === 'string' ? /^(\d{4})-(\d{2})-(\d{2})/.exec(yyyyMmDd) : null
    return match ? match[3] + '.' + match[2] + '.' + match[1] : ''
}

// An RFC 3339 instant becomes DD.MM.YYYY HH:MM in Europe/Berlin.
function dateTime(rfc3339) {
    if (typeof rfc3339 !== 'string' || rfc3339 === '') return ''
    const at = new Date(rfc3339)
    if (isNaN(at.getTime())) return ''
    const parts = {}
    new Intl.DateTimeFormat('de-DE', {timeZone: 'Europe/Berlin', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23'})
        .formatToParts(at).forEach(function (part) { parts[part.type] = part.value })
    return parts.day + '.' + parts.month + '.' + parts.year + ' ' + parts.hour + ':' + parts.minute
}

function carrier(key) {
    return typeof key === 'string' ? (FIXED_CARRIERS[key] || key) : ''
}

// kWh for every carrier but water, which is counted in m³.
function unit(key) {
    return key === 'water' ? 'm³' : 'kWh'
}

function status(key) {
    return typeof key === 'string' ? (FIXED_STATUSES[key] || key) : ''
}

// Leading spaces of a cell are dropped, so the depth is drawn with figure spaces.
function indent(depth) {
    return typeof depth === 'number' && depth > 0 ? '   '.repeat(Math.min(Math.floor(depth), 50)) : ''
}
` + fixedHelpersEnd

// FixedTemplateHelperNames are the names the fixed helpers are registered under.
// A test holds the list against the declarations in FixedTemplateHelpers.
var FixedTemplateHelperNames = []string{"number", "date", "dateTime", "carrier", "unit", "status", "indent"}

// ComposeTemplateHelpers puts the fixed helpers in front of the author's. A fixed
// block the text already carries is replaced, so it never appears twice.
func ComposeTemplateHelpers(author string) string {
	return FixedTemplateHelpers + "\n" + AuthorTemplateHelpers(author)
}

// AuthorTemplateHelpers is the stored helpers text without the fixed block. Text
// that does not start with it, such as a template made elsewhere, is returned as is.
func AuthorTemplateHelpers(stored string) string {
	if !strings.HasPrefix(stored, fixedHelpersBegin) {
		return stored
	}
	end := strings.Index(stored, fixedHelpersEnd)
	if end < 0 {
		return stored
	}
	return strings.TrimPrefix(stored[end+len(fixedHelpersEnd):], "\n")
}

var functionDeclaration = regexp.MustCompile(`\bfunction\s*\*?\s*([A-Za-z_$][\w$]*)\s*\(`)

// CheckAuthorHelpers refuses an author's helpers that declare a function under the
// name of a fixed helper, since the later declaration would replace it. Matching is
// textual, so the name inside a comment or a string is refused as well.
func CheckAuthorHelpers(author string) error {
	for _, match := range functionDeclaration.FindAllStringSubmatch(AuthorTemplateHelpers(author), -1) {
		for _, name := range FixedTemplateHelperNames {
			if match[1] == name {
				return fmt.Errorf("helpers must not declare %q, it is one of the fixed helpers (%s)", name, strings.Join(FixedTemplateHelperNames, ", "))
			}
		}
	}
	return nil
}
