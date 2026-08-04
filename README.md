# reporting-service

Generate swagger docs:

    swag init -g api.go -o docs -dir pkg/api --parseDependency --ot json

## Configuration Variables

- SENERGY_DB_URL
- SENERGY_DB_PORT
- JSREPORT_SERVER_URL
- JSREPORT_SERVER_PORT
- MONGODB_URI
- MONGODB_DATABASE — database to store reports and report jobs in, default `reporting`
- SCHEDULER_TICKER_DURATION — how often to look for due reports, default `1m`
- REPORT_JOB_WORKERS — how many reports may be built at the same time, default `2`
- REPORT_JOB_RETENTION — how long a finished report job stays queryable, default `168h`
- REPORT_JOB_STALE_AFTER — when a running job without heartbeat counts as
  interrupted, default `2m`. Has to stay above the 15s heartbeat interval.

## Tests

    go test ./...

The tests around the report job queue need a mongodb. `docker compose up -d mongodb`
provides one; set `MONGO_TEST_URL` to point somewhere else. They run against a
separate `reporting_test` database and never touch the one the service uses.

Without a mongodb those tests skip, which is also what `go test -short ./...` does.
Set `REQUIRE_MONGO=1` to turn a missing database into a failure instead — CI does
that so a broken service container cannot make the suite look green.

## Creating reports

Report creation is asynchronous, because collecting the data and rendering the file
can both take a while.

`POST /report/create` stores the report model, queues the actual work and answers
`202` right away:

```json
{
  "id": "8f14e45f-ceea-467a-9e2b-6a1d9e0f3c7b",
  "jobId": "2b1a9c74-4f0e-4d31-9c8e-51b6a2f0d3e9"
}
```

The job can then be polled at `GET /report/job/:jobId`:

```json
{
  "data": {
    "id": "2b1a9c74-4f0e-4d31-9c8e-51b6a2f0d3e9",
    "reportId": "8f14e45f-ceea-467a-9e2b-6a1d9e0f3c7b",
    "status": "running",
    "step": "collecting_data",
    "createdAt": "2026-08-04T09:12:44Z",
    "startedAt": "2026-08-04T09:12:44Z"
  }
}
```

`status` is one of `pending`, `running`, `done` or `failed`. While running, `step`
is `collecting_data`, `rendering` or `emailing`. A `done` job carries the
`reportFileId` that `GET /report/file/:reportId/:fileId` serves, a `failed` job
carries `error`.

`GET /report/job?reportId=<id>&limit=<n>` lists the newest jobs of the calling user,
which is how a client picks up the status again after a reload.

Scheduled reports go through the same queue, so both entry points share one
concurrency limit against jsreport and the timescale wrapper.

## jsreport reports

The reports of the jsreport folder `/senergy_reports` and `jsreport-sync`, the
tool that keeps them in sync with an instance, live in
[report-sync](https://github.com/SENERGY-Platform/report-sync).

## jsreport authentication

This service forwards a user token to jsreport, which validates it through
keycloak's token introspection endpoint. Since **keycloak 26.6.2** that endpoint
only accepts a token whose `aud` contains the client doing the introspection —
the fix for CVE-2026-37979. So `jsreport-api` has to be in the audience of every
token that reaches jsreport.

Two clients issue such tokens:

- `frontend` — the user token from the web ui, used for the template endpoints
- `reporting-service` — the token the report job workers exchange, used for
  creating report files and downloading them

Both need a client scope with an *Audience* mapper for `jsreport-api`, assigned as
a default scope. Without it keycloak answers the introspection with
`{"active": false}`, jsreport replies `401` with an empty body and this service
logs `could not get templates ... jsreport-unauthorized`. Keycloak logs the reason:

    type="INTROSPECT_TOKEN_ERROR" error="invalid_token"
    reason="Client 'jsreport-api' is not in the token audience" token_issued_for="frontend"

## Example
### GET /templates
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

### GET /templates/:id
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

### POST /report
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
actual payload to be sent to report server:

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