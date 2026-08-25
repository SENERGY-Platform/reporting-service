# Report job design

Why the job model looks the way it does. The README's
[Creating reports](../README.md#creating-reports) section describes the flow and
the status values; this document carries only the decisions behind it, each of
which had a cheaper-looking alternative.

## Applies when

Changing anything about how report jobs are stored, scheduled, executed or
recovered.

**Not this if**: you are looking for the generic mechanics of a MongoDB-backed
job queue. Those are not specific to this service.

## The job is its own collection

Not a status field on the report model. That separates runtime state from
configuration, tolerates parallel runs of the same report, and keeps a failure
history. A status field on the report has room for none of the three.

## Token exchange happens in the worker

Not with the request bearer. A report can run for minutes and would otherwise
fail halfway through on an expired token. The scheduler already worked this way,
so doing the same in the HTTP path leaves one code path instead of two.

## The scheduler shares the queue with the HTTP path

One concurrency limit then covers the rendering service and the Timescale
wrappers. Two independent queues would mean two independent load sources hitting
the same two dependencies, and neither would know about the other.

## Running renders are abandoned on shutdown

Not awaited — otherwise a long report drags pod termination past the grace
period. The mechanism is a heartbeat every 15s and reaping after
`REPORT_JOB_STALE_AFTER` without one.

**There is deliberately no automatic retry.** An aborted job may already have
written a file, so a retry would produce a second file for the same report. This
is the least obvious of the four decisions and the one most likely to be
"fixed" by someone adding a retry.

## The job API is a contract

Consumers poll the status and fetch the file list once a job reports done. A
change to endpoints, status values or payload breaks them **silently** — nothing
in this repository's build or tests notices. Treat any such change as a breaking
change that has to be coordinated, not shipped alone.
