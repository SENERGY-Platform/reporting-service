# reporting-service

Generate swagger docs:

    swag init -g api.go -o docs -dir pkg/api --parseDependency --ot json

## Configuration Variables

- SENERGY_DB_URL
- SENERGY_DB_PORT
- JSREPORT_SERVER_URL
- JSREPORT_SERVER_PORT

## jsreport reports

The reports of the jsreport folder `/senergy_reports` live in
[report-templates](https://github.com/SENERGY-Platform/report-templates), one
file per syncable property of template, script, data entity and asset. Git is the
source of truth for them, so a change is made locally, tested locally and then
committed there.

`jsreport-sync` moves those files between a jsreport instance and a checkout of
that repository. It only writes the content carrying properties of entities
inside the managed folder: no engine or recipe, no links between the entities, no
permissions, no ids, and it never deletes. Entities that exist only in the
instance are reported and left alone.

    go run ./cmd/jsreport-sync <pull|push|diff> [flags]

| command | effect |
| --- | --- |
| `pull` | write the entities of the instance into the local directory |
| `push` | apply the local files to the instance |
| `diff` | report what `push` would change, exit code 1 on drift |

Run it from the report-templates checkout, or point `-dir` at it:

    go run ./cmd/jsreport-sync pull -user admin -password password -dir ../report-templates

Reading the files from a git ref instead of a checkout works as well, which is
what a run outside of a checkout uses:

    go run ./cmd/jsreport-sync push -from-git main -url http://jsreport.example:5488

### Flags and environment

Every flag has an environment variable, so the command needs no arguments where
the environment is set up.

| flag | variable | default |
| --- | --- | --- |
| `-url` | `JSREPORT_URL` | `http://localhost:5488` |
| `-user`, `-password` | `JSREPORT_USER`, `JSREPORT_PASSWORD` | empty |
| `-token` | `JSREPORT_TOKEN` | empty, takes precedence over basic auth |
| `-folder` | `JSREPORT_FOLDER` | `/senergy_reports` |
| `-dir` | `JSREPORT_SYNC_DIR` | `.` |
| `-from-git` | `JSREPORT_SYNC_REF` | empty, read `-dir` instead |
| `-repo` | `JSREPORT_SYNC_REPO` | `SENERGY-Platform/report-templates` |
| `-repo-token` | `JSREPORT_SYNC_REPO_TOKEN` | empty, only for private repositories |
| `-create` | `JSREPORT_SYNC_CREATE` | `false` |

An entity that does not exist in the target is only reported. `-create` inserts
scripts, data entities and assets, but never templates, and it does not link them
to a template - a new report is created and linked once in the studio.

A `.data.json` file that is not valid json is reported as `invalid` and not
written, so a broken sample data file cannot take a report's preview down.

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