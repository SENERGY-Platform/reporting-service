# reporting-service

Builds reports from the platform's time series and device data and renders them
to files through jsreport. A report is a template plus typed data fields; fields
can hold literal values or a query that this service resolves before rendering.

Reports are created on request or on a schedule, and the result is a file a user
can download. Creation is **asynchronous** — collecting the data and rendering
can each take minutes — so the API hands out a job and the client polls it.

Written in Go. Reports, jobs and the queue live in MongoDB; the rendering itself
happens in jsreport.

## Creating a report

`POST /report/create` stores the report model, queues the work and answers `202`
right away:

```json
{
  "id": "8f14e45f-ceea-467a-9e2b-6a1d9e0f3c7b",
  "jobId": "2b1a9c74-4f0e-4d31-9c8e-51b6a2f0d3e9"
}
```

Poll the job at `GET /report/job/:jobId`:

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

`status` is one of `pending`, `running`, `done` or `failed`. While running,
`step` is `collecting_data`, `rendering` or `emailing`. A `done` job carries the
`reportFileId` that `GET /report/file/:reportId/:fileId` serves; a `failed` job
carries `error`.

`GET /report/job?reportId=<id>&limit=<n>` lists the newest jobs of the calling
user, which is how a client picks the status up again after a reload.

Scheduled reports go through the same queue, so both entry points share one
concurrency limit against jsreport and the Timescale wrapper.

## Configuration

| Variable | Meaning |
|---|---|
| `SENERGY_DB_URL`, `SENERGY_DB_PORT` | the Timescale wrapper the report data is read from |
| `JSREPORT_SERVER_URL`, `JSREPORT_SERVER_PORT` | the rendering service |
| `MONGODB_URI` | connection for reports, jobs and the queue |
| `MONGODB_DATABASE` | database name, default `reporting` |
| `KEYCLOAK_CLIENT_ID` | the client this service exchanges its worker token as, default `reporting-service` |
| `SCHEDULER_TICKER_DURATION` | how often to look for due reports, default `1m` |
| `REPORT_JOB_WORKERS` | how many reports may be built at the same time, default `2` |
| `REPORT_JOB_RETENTION` | how long a finished job stays queryable, default `168h` |
| `REPORT_JOB_STALE_AFTER` | when a running job without heartbeat counts as interrupted, default `2m`. **Has to stay above the 15s heartbeat interval.** |

## Tests

```bash
go test ./...
```

The tests around the report job queue need a MongoDB. `docker compose up -d mongodb`
provides one; set `MONGO_TEST_URL` to point elsewhere. They run against a separate
`reporting_test` database and never touch the one the service uses.

Without a MongoDB those tests skip, which is also what `go test -short ./...` does.
Set `REQUIRE_MONGO=1` to turn a missing database into a failure instead — CI does
that, so a broken service container cannot make the suite look green.

## API documentation

Generated from the annotations:

```bash
swag init -g api.go -o docs -dir pkg/api --parseDependency --ot json
```

## jsreport templates

The report templates are not part of this repository. This service assumes they
exist in the jsreport instance it talks to.

## jsreport authentication

This service forwards a token to jsreport, which validates it through Keycloak's
token introspection endpoint. Since **Keycloak 26.6.2** that endpoint only
accepts a token whose `aud` contains the client doing the introspection — the fix
for CVE-2026-37979. So **the client jsreport introspects as has to be in the
audience of every token that reaches jsreport.**

Two paths carry such tokens, and both have to satisfy that:

- the **user token** from the frontend, used for the template endpoints
- the **worker token** this service exchanges (`KEYCLOAK_CLIENT_ID`), used for
  creating and downloading report files

Each issuing client needs a client scope with an *Audience* mapper for jsreport's
client, assigned as a default scope. Satisfying only one path leaves the other
failing the same way under a different `token_issued_for` — which reads like an
intermittent fault rather than a missing mapper.

Without it, Keycloak answers the introspection with `{"active": false}`, jsreport
replies `401` with an empty body, and this service logs
`could not get templates ... jsreport-unauthorized`. The reason is only visible in
Keycloak's own log:

    type="INTROSPECT_TOKEN_ERROR" error="invalid_token"
    reason="Client '<jsreport client>' is not in the token audience"
    token_issued_for="<issuing client>"

The concrete client names are deployment configuration and differ per
installation, so they are not listed here.

## Further documentation

`docs/` holds hand-written knowledge next to the generated `swagger.json`:

- [report job design](docs/report-job-design.md) — the four decisions behind the
  job model, including why there is no automatic retry
- [payload examples](docs/payload-examples.md) — template and report payloads,
  and what reaches the rendering service
