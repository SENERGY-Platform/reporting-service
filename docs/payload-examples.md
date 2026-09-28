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
