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
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SENERGY-Platform/reporting-service/lib"
	"go.mongodb.org/mongo-driver/bson"
)

func TestEnqueueReportFileCreationCreatesTheReportModel(t *testing.T) {
	requireMongo(t)
	client := testClient(t, &fakeDriver{})
	token := testToken(t, "user-1")

	reportId, jobId, err := client.EnqueueReportFileCreation(lib.Report{
		Name:         "Verbrauch",
		TemplateName: "consumption",
	}, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reportId == "" {
		t.Error("no report id was returned, the ui needs it right away")
	}
	if jobId == "" {
		t.Error("no job id was returned")
	}

	report, err := client.GetReportModel(reportId, token)
	if err != nil {
		t.Fatalf("report model was not stored: %v", err)
	}
	if report.Name != "Verbrauch" {
		t.Errorf("name = %q, want %q", report.Name, "Verbrauch")
	}

	job, err := client.GetReportJob(jobId, token)
	if err != nil {
		t.Fatalf("job was not stored: %v", err)
	}
	if job.Status != lib.ReportJobPending {
		t.Errorf("status = %q, want %q", job.Status, lib.ReportJobPending)
	}
	if job.ReportId != reportId {
		t.Errorf("job reportId = %q, want %q", job.ReportId, reportId)
	}
	if job.Done() {
		t.Error("a pending job reports itself as done")
	}
}

func TestEnqueueReportFileCreationKeepsAnExistingReport(t *testing.T) {
	requireMongo(t)
	client := testClient(t, &fakeDriver{})
	token := testToken(t, "user-1")

	existing, err := client.SaveReportModel(lib.Report{Name: "erst"}, token)
	if err != nil {
		t.Fatalf("could not store report: %v", err)
	}

	reportId, _, err := client.EnqueueReportFileCreation(lib.Report{Id: existing.Id, Name: "dann"}, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reportId != existing.Id {
		t.Errorf("report id = %q, want the existing %q", reportId, existing.Id)
	}

	reports, err := client.GetReportModels(token, map[string][]string{}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reports) != 1 {
		t.Errorf("stored %d reports, want 1", len(reports))
	}
}

func TestGetReportJobIsScopedToTheUser(t *testing.T) {
	requireMongo(t)
	client := testClient(t, &fakeDriver{})

	_, jobId, err := client.EnqueueReportFileCreation(lib.Report{Name: "geheim"}, testToken(t, "owner"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err = client.GetReportJob(jobId, testToken(t, "someone-else")); !errors.Is(err, ErrJobNotFound) {
		t.Errorf("err = %v, want ErrJobNotFound", err)
	}
}

func TestGetReportJobReportsAnUnknownJob(t *testing.T) {
	requireMongo(t)
	client := testClient(t, &fakeDriver{})

	if _, err := client.GetReportJob("does-not-exist", testToken(t, "user-1")); !errors.Is(err, ErrJobNotFound) {
		t.Errorf("err = %v, want ErrJobNotFound", err)
	}
}

func TestGetReportJobsFiltersAndLimits(t *testing.T) {
	requireMongo(t)
	client := testClient(t, &fakeDriver{})
	token := testToken(t, "user-1")

	reportA, _, err := client.EnqueueReportFileCreation(lib.Report{Name: "a"}, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, _, err = client.EnqueueReportFileCreation(lib.Report{Id: reportA, Name: "a"}, token); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	reportB, _, err := client.EnqueueReportFileCreation(lib.Report{Name: "b"}, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	all, err := client.GetReportJobs(token, "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("got %d jobs, want 3", len(all))
	}

	forA, err := client.GetReportJobs(token, reportA, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(forA) != 2 {
		t.Fatalf("got %d jobs for report a, want 2", len(forA))
	}
	for _, job := range forA {
		if job.ReportId != reportA {
			t.Errorf("job of report %q leaked into the list of %q", job.ReportId, reportA)
		}
	}

	limited, err := client.GetReportJobs(token, "", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(limited) != 1 {
		t.Errorf("got %d jobs, want the limit of 1", len(limited))
	}

	other, err := client.GetReportJobs(testToken(t, "user-2"), "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(other) != 0 {
		t.Errorf("got %d jobs of another user, want 0", len(other))
	}
	_ = reportB
}

func TestGetReportJobsCapsTheLimit(t *testing.T) {
	requireMongo(t)
	client := testClient(t, &fakeDriver{})
	token := testToken(t, "user-1")
	if _, _, err := client.EnqueueReportFileCreation(lib.Report{Name: "a"}, token); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// A request for more than the cap has to be answered, not rejected.
	jobs, err := client.GetReportJobs(token, "", MaxReportJobLimit*10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("got %d jobs, want 1", len(jobs))
	}
}

func TestClaimNextJobHandsOutEachJobOnce(t *testing.T) {
	requireMongo(t)
	client := testClient(t, &fakeDriver{})
	token := testToken(t, "user-1")

	if _, _, err := client.EnqueueReportFileCreation(lib.Report{Name: "a"}, token); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	first, ok := client.claimNextJob()
	if !ok {
		t.Fatal("the queued job was not claimed")
	}
	if first.Status != lib.ReportJobRunning {
		t.Errorf("status = %q, want %q", first.Status, lib.ReportJobRunning)
	}
	if first.StartedAt == nil {
		t.Error("startedAt was not set on claim")
	}

	if _, ok = client.claimNextJob(); ok {
		t.Error("the same job was claimed twice")
	}
}

func TestClaimNextJobTakesTheOldestFirst(t *testing.T) {
	requireMongo(t)
	client := testClient(t, &fakeDriver{})

	older, err := client.insertReportJob(lib.ReportJob{ReportId: "older", UserId: "user-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// insertReportJob stamps CreatedAt itself, so the order is forced here.
	ctx, cancel := dbCtx()
	defer cancel()
	if _, err = ReportJobs().UpdateOne(ctx, bson.M{"_id": older.Id},
		bson.M{"$set": bson.M{"createdat": time.Now().Add(-time.Hour)}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err = client.insertReportJob(lib.ReportJob{ReportId: "newer", UserId: "user-1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	claimed, ok := client.claimNextJob()
	if !ok {
		t.Fatal("no job was claimed")
	}
	if claimed.ReportId != "older" {
		t.Errorf("claimed the job of %q, want the oldest one", claimed.ReportId)
	}
}

func TestRunningJobRendersTheReportAndReportsDone(t *testing.T) {
	requireMongo(t)
	driver := &fakeDriver{}
	client := testClient(t, driver)
	token := testToken(t, "user-1")

	reportId, jobId, err := client.EnqueueReportFileCreation(lib.Report{
		Name:         "Verbrauch",
		TemplateName: "consumption",
		Data: map[string]lib.ReportObject{
			"title": {DataType: lib.DataType{ValueType: "string"}, Value: "Januar"},
		},
	}, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	job := runOneJob(t, client)
	if job.Id != jobId {
		t.Fatalf("ran job %q, want %q", job.Id, jobId)
	}

	finished, err := client.GetReportJob(jobId, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if finished.Status != lib.ReportJobDone {
		t.Fatalf("status = %q, error = %q, want %q", finished.Status, finished.Error, lib.ReportJobDone)
	}
	if finished.ReportFileId == "" {
		t.Error("no report file id was recorded")
	}
	if finished.FinishedAt == nil {
		t.Error("finishedAt was not set")
	}
	if finished.Step != "" {
		t.Errorf("step = %q, want it cleared on a finished job", finished.Step)
	}

	renders := driver.renders()
	if len(renders) != 1 {
		t.Fatalf("rendered %d reports, want 1", len(renders))
	}
	if renders[0].TemplateName != "consumption" {
		t.Errorf("template = %q, want %q", renders[0].TemplateName, "consumption")
	}
	if renders[0].Data["title"] != "Januar" {
		t.Errorf("data = %v, want the title of the request", renders[0].Data)
	}

	// The rendered file has to end up on the report, otherwise the ui cannot
	// offer it for download.
	report, err := client.GetReportModel(reportId, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.ReportFiles) != 1 {
		t.Fatalf("report has %d files, want 1", len(report.ReportFiles))
	}
	if report.ReportFiles[0].Id != finished.ReportFileId {
		t.Errorf("report file %q does not match the job's %q", report.ReportFiles[0].Id, finished.ReportFileId)
	}
}

func TestRunningJobRecordsAFailedRender(t *testing.T) {
	requireMongo(t)
	driver := &fakeDriver{renderErr: errors.New("jsreport-api: template not found")}
	client := testClient(t, driver)
	token := testToken(t, "user-1")

	_, jobId, err := client.EnqueueReportFileCreation(lib.Report{Name: "kaputt"}, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	runOneJob(t, client)

	job, err := client.GetReportJob(jobId, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.Status != lib.ReportJobFailed {
		t.Fatalf("status = %q, want %q", job.Status, lib.ReportJobFailed)
	}
	if job.Error != "jsreport-api: template not found" {
		t.Errorf("error = %q, want the message of the failing step", job.Error)
	}
	if !job.Done() {
		t.Error("a failed job does not report itself as done")
	}
}

func TestRunningJobKeepsTheReportOfANewlyCreatedModel(t *testing.T) {
	requireMongo(t)
	client := testClient(t, &fakeDriver{})
	token := testToken(t, "user-1")

	reportId, jobId, err := client.EnqueueReportFileCreation(lib.Report{Name: "neu"}, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	runOneJob(t, client)

	// The worker runs with an exchanged token, so the report must still belong to
	// the user who asked for it.
	report, err := client.GetReportModel(reportId, token)
	if err != nil {
		t.Fatalf("report is not readable by its owner: %v", err)
	}
	if report.UserId != "user-1" {
		t.Errorf("userId = %q, want %q", report.UserId, "user-1")
	}
	job, err := client.GetReportJob(jobId, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.Status != lib.ReportJobDone {
		t.Errorf("status = %q, error = %q, want %q", job.Status, job.Error, lib.ReportJobDone)
	}
}

func TestReapStaleJobsFailsAJobWhoseWorkerDied(t *testing.T) {
	requireMongo(t)
	client := testClient(t, &fakeDriver{})
	token := testToken(t, "user-1")

	_, jobId, err := client.EnqueueReportFileCreation(lib.Report{Name: "a"}, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := client.claimNextJob(); !ok {
		t.Fatal("the queued job was not claimed")
	}

	// Simulate a process that was killed while rendering: the job stays running
	// but no heartbeat arrives any more.
	ctx, cancel := dbCtx()
	defer cancel()
	if _, err = ReportJobs().UpdateOne(ctx, bson.M{"_id": jobId},
		bson.M{"$set": bson.M{"heartbeat": time.Now().Add(-10 * time.Minute)}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	client.reapStaleJobs(2 * time.Minute)

	job, err := client.GetReportJob(jobId, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.Status != lib.ReportJobFailed {
		t.Errorf("status = %q, want %q", job.Status, lib.ReportJobFailed)
	}
	if job.Error == "" {
		t.Error("no reason was recorded on the reaped job")
	}
}

func TestReapStaleJobsLeavesAHealthyJobAlone(t *testing.T) {
	requireMongo(t)
	client := testClient(t, &fakeDriver{})
	token := testToken(t, "user-1")

	_, jobId, err := client.EnqueueReportFileCreation(lib.Report{Name: "a"}, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := client.claimNextJob(); !ok {
		t.Fatal("the queued job was not claimed")
	}

	client.reapStaleJobs(2 * time.Minute)

	job, err := client.GetReportJob(jobId, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.Status != lib.ReportJobRunning {
		t.Errorf("status = %q, want the job to still be running", job.Status)
	}
}

func TestFinishJobDoesNotResurrectAnInterruptedJob(t *testing.T) {
	requireMongo(t)
	client := testClient(t, &fakeDriver{})
	token := testToken(t, "user-1")

	_, jobId, err := client.EnqueueReportFileCreation(lib.Report{Name: "a"}, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := client.claimNextJob(); !ok {
		t.Fatal("the queued job was not claimed")
	}

	client.failInFlightJobs("interrupted by a service restart")
	// The abandoned render finishing afterwards must not flip the job back.
	client.finishJob(jobId, "file-1", nil)

	job, err := client.GetReportJob(jobId, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.Status != lib.ReportJobFailed {
		t.Errorf("status = %q, want it to stay %q", job.Status, lib.ReportJobFailed)
	}
}

func TestRunJobWorkersPicksUpAQueuedJob(t *testing.T) {
	requireMongo(t)
	client := testClient(t, &fakeDriver{})
	token := testToken(t, "user-1")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	workersDone := make(chan error, 1)
	go func() { workersDone <- client.RunJobWorkers(ctx) }()

	_, jobId, err := client.EnqueueReportFileCreation(lib.Report{Name: "a", TemplateName: "t"}, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	job := waitForJob(t, client, jobId, token, 10*time.Second)
	if job.Status != lib.ReportJobDone {
		t.Errorf("status = %q, error = %q, want %q", job.Status, job.Error, lib.ReportJobDone)
	}

	cancel()
	select {
	case <-workersDone:
	case <-time.After(10 * time.Second):
		t.Error("RunJobWorkers did not return after its context was cancelled")
	}
}

func TestRunJobWorkersRejectsAStaleTimeoutBelowTheHeartbeat(t *testing.T) {
	client := testClient(t, &fakeDriver{})
	client.Config.ReportJobStaleAfter = "1s"
	if err := client.RunJobWorkers(context.Background()); err == nil {
		t.Error("got no error for a stale timeout below the heartbeat interval, want one")
	}
}

func TestRunJobWorkersRejectsAnInvalidStaleTimeout(t *testing.T) {
	client := testClient(t, &fakeDriver{})
	client.Config.ReportJobStaleAfter = "soon"
	if err := client.RunJobWorkers(context.Background()); err == nil {
		t.Error("got no error for an unparsable stale timeout, want one")
	}
}

func TestHasUnfinishedJob(t *testing.T) {
	requireMongo(t)
	client := testClient(t, &fakeDriver{})
	token := testToken(t, "user-1")

	reportId, jobId, err := client.EnqueueReportFileCreation(lib.Report{Name: "a"}, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	unfinished, err := client.hasUnfinishedJob(reportId)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !unfinished {
		t.Error("a pending job is not reported as unfinished")
	}

	if _, ok := client.claimNextJob(); !ok {
		t.Fatal("the queued job was not claimed")
	}
	client.finishJob(jobId, "file-1", nil)

	unfinished, err = client.hasUnfinishedJob(reportId)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unfinished {
		t.Error("a finished job is still reported as unfinished")
	}
}

// runOneJob claims and executes exactly one queued job and returns it.
func runOneJob(t *testing.T, client *Client) lib.ReportJob {
	t.Helper()
	job, ok := client.claimNextJob()
	if !ok {
		t.Fatal("no job was queued")
	}
	client.executeJob(context.Background(), job)
	return job
}

func waitForJob(t *testing.T, client *Client, jobId string, token string, timeout time.Duration) lib.ReportJob {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		job, err := client.GetReportJob(jobId, token)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if job.Done() {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %q did not finish within %v, last status %q", jobId, timeout, job.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
