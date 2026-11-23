# Payload examples

Reference payloads for the template and report endpoints, moved out of the
README so that stays readable. The endpoint reference itself is generated into
`swagger.json`.

## Applies when

Building a client against the template and report endpoints, or checking what a
`valueType` and a `query` block turn into on the way to the rendering service.

**Not this if**: you need the asynchronous creation flow — that is
[Creating reports](../README.md#creating-reports) in the README, with the design
behind it in [report job design](report-job-design.md).

## GET /templates

```json
{
  "data": [
    {
      "name": "test",
      "id": "lNrdyWKHZnDQEP8X",
      "data": {}
    },
    {
      "name": "...",
      "id": "...",
      "data": {}
    }
  ]
}
```

## GET /templates/:id

The template carries both the raw JSON string it was defined with and a
structured description derived from it. A template written through `/templates`
also carries `helpers`, the author's text without the fixed helpers (see
[Writing templates](#writing-templates)).

```json
{
    "data": {
        "name": "test",
        "id": "lNrdyWKHZnDQEP8X",
        "data": {
            "name": "test-data",
            "id": "aTwVzIETUniSJBkd",
            "dataJsonString": "{\n    \"test\": \"test\",\n    \"test2\": {\"test3\": 2},\n    \"test4\": [{\"test5\": \"bla\"}],\n    \"test6\": \n    [\n        \n    ],\n    \"test8\": \n    [\n        \n    ]\n}",
            "dataStructured": {
                "test": {
                    "name": "test",
                    "valueType": "string"
                },
                "test2": {
                    "name": "test2",
                    "valueType": "object",
                    "fields": {
                        "test3": {
                            "name": "test3",
                            "valueType": "float64"
                        }
                    }
                },
                "test4": {
                    "name": "test4",
                    "valueType": "array",
                    "length": 1,
                    "children": {
                        "0": {
                            "name": "0",
                            "valueType": "object",
                            "fields": {
                                "test5": {
                                    "name": "test5",
                                    "valueType": "string"
                                }
                            }
                        }
                    }
                },
                "test6": {
                    "name": "test6",
                    "valueType": "array"
                },
                "test8": {
                    "name": "test8",
                    "valueType": "array"
                }
            }
        }
    }
}
```

## POST /report

The request carries typed fields. A field either has a literal `value`, or a
`query` that this service resolves against the Timescale wrapper before
rendering — `test8` below is the second kind.

```json
{
  "id": "test",
  "data": {
    "test": {
      "name": "test",
      "valueType": "string",
      "value": "test"
    },
    "test2": {
      "name": "test2",
      "valueType": "object",
      "fields": {
        "test3": {
          "name": "test3",
          "valueType": "int",
          "value": 3
        }
      }
    },
    "test4": {
      "name": "test4",
      "valueType": "array",
      "children": {
        "test5": {
          "name": "test5",
          "valueType": "string",
          "value": "blsssa"
        },
        "test7": {
          "name": "test7",
          "valueType": "int",
          "value": 1
        }
      }
    },
    "test6": {
      "name": "test6",
      "valueType": "array",
      "value": [
        1,
        2,
        3,
        4,
        5
      ]
    },
    "test8": {
      "name": "test8",
      "valueType": "array",
      "query": {
        "columns": [
          {
            "name": "energy.value",
            "groupType": "difference-last"
          }
        ],
        "time": {
          "last": "12months"
        },
        "groupTime": "1months",
        "serviceId": "urn:infai:ses:service:xy",
        "deviceId": "urn:infai:ses:device:xy"
      }
    }
  }
}
```

### What reaches the rendering service

The typing is stripped: every field becomes its plain value, and a `query`
becomes the resolved series. This is what a template is written against.

```json
{
  "test": "test",
  "test2": {
    "test3": 3
  },
  "test4": [
    {
      "test5": "blsssa",
      "test7": 1
    }
  ],
  "test6": [1,2,3,4,5],
  "test8": [1,2,3,4,5,6,7,8,9,10,11,12]
}
```

### Device group queries

A `query` can target a device group instead of a single device: it carries a
`deviceGroupId`, and its column a `criteria` instead of a `name`. The Timescale
wrapper answers with a list of series, each tagged with a `deviceId` and
`serviceId`, and `queryOptions` decide how they reach the template:

- `deviceGroupMode` is `aggregate` (the default) or `per_device`.
- `aggregation` is `sum` (the default) or `mean`, and only applies to `aggregate`.
  Missing values are skipped, not counted as zero.

`aggregate` combines the first value column of every series into one series, so
the result has the same shape as for a single device. With a `groupTime` the rows
are matched by timestamp and ordered by time. Without one, `aggregate` needs a
`limit` of 1 and combines the latest value of every device.
`aggregate` rejects an `orderColumnIndex` other than 0, since series sorted by
value cannot be merged. `per_device` needs `valueType` `array`.

```json
{
  "consumption": {
    "name": "consumption",
    "valueType": "array",
    "query": {
      "deviceGroupId": "urn:infai:ses:device-group:xy",
      "columns": [
        {
          "criteria": {
            "function_id": "urn:infai:ses:measuring-function:xy",
            "aspect_id": "urn:infai:ses:aspect:xy"
          },
          "groupType": "difference-last"
        }
      ],
      "time": {
        "last": "12months"
      },
      "groupTime": "1months"
    },
    "queryOptions": {
      "deviceGroupMode": "per_device"
    }
  }
}
```

With `per_device` the template receives one entry per series, in the order the
wrapper returned them. `name`
is the device's nickname, else its name, else its id; `values` follow
`resultObject` as for a single device and are `[]` for a device without data.

```json
{
  "consumption": [
    {
      "deviceId": "urn:infai:ses:device:a",
      "serviceId": "urn:infai:ses:service:a",
      "name": "Kitchen",
      "values": [1, 2, 3]
    },
    {
      "deviceId": "urn:infai:ses:device:b",
      "serviceId": "urn:infai:ses:service:b",
      "name": "urn:infai:ses:device:b",
      "values": [4, 5, 6]
    }
  ]
}
```

With `"deviceGroupMode": "aggregate", "aggregation": "sum"` the same object
yields `"consumption": [5, 7, 9]`.

### Figures queries

An object with a `figuresQuery` is resolved from the figures service instead of the
Timescale wrapper and needs `FIGURES_URL`. `valueType` is `object`, and the object
turns into the form below. `period.kind` is `previousDay`, `previousWeek` (Monday
first), `previousMonth`, `previousQuarter`, `previousYear`, `currentMonth`,
`currentYear` or `fixed` with `start` and `end` as days, the end day included. A
period is cut at midnight of `timeZone` (default `Europe/Berlin`) and a `current*`
period ends at the start of the day of the run. `nodeId` narrows the rows to a node
and what is below it, `carriers` the values.

```json
{
  "consumption": {
    "name": "consumption",
    "valueType": "object",
    "figuresQuery": {
      "graphId": "urn:infai:ses:graph:xy",
      "carriers": ["electricity", "gas"],
      "period": { "kind": "previousMonth" }
    }
  }
}
```

What the template receives as `consumption`, here for a run on 2026-11-23:

```json
{
  "graph": { "id": "urn:infai:ses:graph:xy", "name": "Werk" },
  "node": null,
  "period": { "kind": "previousMonth", "start": "2026-10-01", "end": "2026-10-31", "timeZone": "Europe/Berlin" },
  "carriers": ["electricity", "gas"],
  "rows": [
    { "nodeId": "n-site", "name": "Werk", "path": ["Werk"], "depth": 0, "values": { "electricity": 500.5, "gas": 60 }, "share": null, "alsoUnder": [], "generation": { "electricity": 11047 } },
    { "nodeId": "n-gas", "name": "Gaszaehler", "path": ["Werk", "Gaszaehler"], "depth": 1, "values": { "gas": 60 }, "share": 100, "alsoUnder": ["Halle"] }
  ],
  "meters": [
    { "nodeId": "n-gas", "deviceId": "dev-gas", "name": "Gaszaehler", "status": "partial", "first": "2026-10-12", "last": "2026-10-31" }
  ],
  "notMeters": [
    { "nodeId": "n-battery", "deviceId": "dev-battery", "name": "Speicher", "deviceType": "Batteriespeicher" }
  ],
  "generation": [
    { "nodeId": "n-chp", "deviceId": "dev-chp", "name": "BHKW", "carrier": "electricity", "kwh": 11047 }
  ],
  "generatedAt": "2026-11-23T15:30:00+01:00"
}
```

- `rows` follow the tree depth first, children by name. `share` is the weight of the
  edge to the parent the row stands under, in percent, and `null` for a top row.
  A node the graph hangs under several parents appears once, under the first one
  reached, and `alsoUnder` names the others, sorted, so sums over rows do not count
  it twice. A detached node (edge weight 0) is a top row of its own, and a node
  marked `dashboard/excluded` has neither a row nor a meter. `values` are kWh,
  water in m³. A carrier a node never measured is absent.
- `generation` of a row is what the plants at and below the node made, kWh per
  carrier, not weighted by shares, and absent where there are none. Consumption in
  `values` is not reduced by it. The top level `generation` has one entry per plant
  and carrier (`kwh`). A plant is a device with a feed-in register and no draw register
  for the carrier. A grid meter with both registers is none, its feed-in is not
  listed. Water is never generation. `carriers` narrows it like the values.
- `meters` has one entry per device node. `status` is `none` without readings,
  `partial` if the first reading is more than a day after the start or the last more
  than a day before the end, else `complete`. First and last are taken over all
  registers of the meter, one that lacks an end does not hide the others. `first`
  and `last` are days and absent where no register has that end.
- `notMeters` lists device nodes that have no reading and whose device type name
  (from the device repository) has no "meter" or "zähler" in it, such as a storage or
  a CHP unit without a register. They are not in `meters`, since they are no gap. A
  device whose type is unknown, also where the repository does not answer, stays a
  meter.
- A refusal of the graph by the device repository or figures (for example 403 on a
  graph the report owner may not read) fails the run with a message naming the
  service, the status and the start of the answer. Devices the owner may not read are
  left out by figures without a sign in its answer, so their meter shows `none`.
- Each call to the device repository and figures is bound by `FIGURES_TIMEOUT`,
  default `2m`.

## Writing templates

Needs `TEMPLATE_FOLDER` and `TEMPLATE_AUTHOR_ROLE` (otherwise `404`) and the
realm role in the caller's token (otherwise `403`). A caller works in the
subfolder `TEMPLATE_FOLDER/<group>`, where `<group>` is the value of the claim
`TEMPLATE_GROUP_CLAIM` (default `group`) in the token, for example
`custom_reports/acme`. The group needs read and edit permission on that
subfolder. Fields the body does not
list, such as `engine` or `scripts`, are refused with `400`. Every call to
jsreport carries the caller's token, so jsreport's permissions decide.

`POST /templates` creates the sample data and the template in the group's folder and
links them. The data entity is named `<name> data`. `recipe` is `chrome-pdf` or
`html-to-xlsx`, the engine is always handlebars. The answer is `201` with the
template as in `GET /templates/:id`.

```json
{
  "name": "Energieverbrauch Monat",
  "recipe": "html-to-xlsx",
  "content": "<table>{{#each consumption.rows}}<tr><td>{{name}}</td></tr>{{/each}}</table>",
  "sampleData": { "consumption": { "rows": [{ "name": "Werk" }] } }
}
```

Limits:

| Field | Limit |
|---|---|
| `name` | 100 characters, not empty, no `<>:"/\|?*` or control characters, no space at either end |
| `content` | not empty, 512 KiB |
| `helpers` | optional, the JavaScript of the handlebars helpers, 64 KiB, without a function named like a fixed helper |
| `sampleData` | a JSON object, 256 KiB |
| request body | 2 MiB |

### Fixed helpers

Every template written through `POST` or `PUT /templates` gets the helpers below in
front of its own, in the `helpers` field of the jsreport template (the constant
`FixedTemplateHelpers` in `lib/template_helpers.go`). `GET /templates/:id` and the
answers of `POST` and `PUT` return only the author's text, and a write puts the set
in front again, never twice. A preview of a draft renders with them too. Templates
that report-sync keeps are not touched.

| Helper | Output | Example |
|---|---|---|
| `{{number value decimals}}` | German format with thousands dots, 0 decimals by default, empty for a missing or non-numeric value | `{{number 1234567.891 2}}` gives `1.234.567,89`, `{{number 0.5}}` gives `1` |
| `{{date day}}` | `YYYY-MM-DD` as `DD.MM.YYYY`, empty otherwise | `2026-10-31` gives `31.10.2026` |
| `{{dateTime instant}}` | RFC 3339 instant as `DD.MM.YYYY HH:MM` in Europe/Berlin | `2026-07-01T10:05:00Z` gives `01.07.2026 12:05` |
| `{{carrier key}}` | `electricity` Strom, `gas` Gas, `biogas` Biogas, `oil` Heizöl, `heat` Wärme, `water` Wasser, another key as it is | `water` gives `Wasser` |
| `{{unit key}}` | `m³` for `water`, else `kWh` | `gas` gives `kWh` |
| `{{status key}}` | `none` ohne Daten, `partial` Teilzeitraum, `complete` vollständig | `partial` gives `Teilzeitraum` |
| `{{indent depth}}` | three figure spaces (U+2007) per level, since a cell drops leading spaces | `{{indent depth}}{{name}}` |

Only function declarations become helpers in jsreport, so the author's helpers can
call the fixed ones, as in `function share(v) { return number(v, 1) + ' %' }`. A
helper that declares a function with the name of a fixed one (`number`, `date`,
`dateTime`, `carrier`, `unit`, `status`, `indent`) is refused with `400`, also in a
preview. The check is textual, so the name in a comment or a string is refused too.

`PUT /templates/:id` takes the same body and answers `200`, `DELETE /templates/:id`
removes template and sample data and answers `204`. Both only work on a template
in the group's folder, and a sample data entity elsewhere is left alone (an update
then gives the template a new one). Names are unique per folder across templates and
data, compared without regard to case, and are checked before anything is written.
An update writes the template, then the sample data, and writes the template back if
the second step fails.

Errors, as `{"error": "<sentence>"}`. The 403 sentences are fixed so that a caller can
tell the reasons apart:

| Status | Sentence | Reason |
|---|---|---|
| 400 | the reason, for example `name is longer than 100 characters` | invalid input |
| 401 | `report engine rejected the authorization` | jsreport did not accept the token |
| 403 | `the template author role is missing` | the token lacks `TEMPLATE_AUTHOR_ROLE` |
| 403 | `the account has no report group` | the claim `TEMPLATE_GROUP_CLAIM` is missing or empty in the token |
| 403 | `template folder not readable` | the caller cannot see `TEMPLATE_FOLDER` or its subfolder for the group |
| 403 | `no write access to the template folder` | creating was refused by jsreport |
| 403 | `no write access to the template` | changing or deleting was refused by jsreport |
| 403 | `only templates in the template folder can be changed` | the template lies outside the group's folder, for example in another group's |
| 403 | `report engine denied access` | a preview was refused by jsreport |
| 404 | `writing templates is not configured` | `TEMPLATE_FOLDER` or the role is not set |
| 404 | `template not found` | the id is unknown or the caller may not read it |
| 409 | `a template or data with this name already exists: ...` | name taken in the folder |
| 422 | `could not collect the preview data: ...` | a preview's `data` could not be resolved |

`GET /templates/:id` answers `404 template not found` for an id jsreport does not
return.

`POST /templates/preview` renders without saving and without a job. Give either a
draft or the id of a stored template, and `data` as in `POST /report`, so a
`figuresQuery` is resolved as in a run:

```json
{
  "template": { "recipe": "chrome-pdf", "content": "<h1>{{consumption.graph.name}}</h1>" },
  "data": {
    "consumption": {
      "name": "consumption",
      "valueType": "object",
      "figuresQuery": { "graphId": "urn:infai:ses:graph:xy", "period": { "kind": "previousMonth" } }
    }
  }
}
```

The answer is a PNG of the top of the page in a viewport of 1200 by 1600 pixels, not
the whole page. With `?as=file` it is the file of the recipe instead, `.pdf` or
`.xlsx`. A stored template is rendered by its id, so its engine and settings apply as
in a run, and without `data` jsreport uses its sample data.
