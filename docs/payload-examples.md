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
structured description derived from it.

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
    { "nodeId": "n-site", "name": "Werk", "path": ["Werk"], "depth": 0, "values": { "electricity": 500.5, "gas": 60 }, "share": null, "alsoUnder": [] },
    { "nodeId": "n-gas", "name": "Gaszaehler", "path": ["Werk", "Gaszaehler"], "depth": 1, "values": { "gas": 60 }, "share": 100, "alsoUnder": ["Halle"] }
  ],
  "meters": [
    { "nodeId": "n-gas", "deviceId": "dev-gas", "name": "Gaszaehler", "status": "partial", "first": "2026-10-12", "last": "2026-10-31" }
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
- `meters` has one entry per device node. `status` is `none` without readings,
  `partial` if the first reading is more than a day after the start or the last more
  than a day before the end, else `complete`. First and last are taken over all
  registers of the meter, one that lacks an end does not hide the others. `first`
  and `last` are days and absent where no register has that end.
- A refusal of the graph by the device repository or figures (for example 403 on a
  graph the report owner may not read) fails the run with a message naming the
  service, the status and the start of the answer. Devices the owner may not read are
  left out by figures without a sign in its answer, so their meter shows `none`.
- Each call to the device repository and figures is bound by `FIGURES_TIMEOUT`,
  default `2m`.
