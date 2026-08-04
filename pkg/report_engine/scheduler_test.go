/*
 * Copyright 2025 InfAI (CC SES)
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
	"testing"
	"time"

	"github.com/SENERGY-Platform/reporting-service/lib"
	"go.mongodb.org/mongo-driver/bson"
)

func TestEnqueueDueReportsQueuesADueReport(t *testing.T) {
	requireMongo(t)
	client := testClient(t, &fakeDriver{})
	token := testToken(t, "user-1")

	report, err := client.SaveReportModel(lib.Report{Name: "monatlich", Cron: "0 6 1 * *"}, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	makeDue(t, report.Id)

	client.enqueueDueReports()

	jobs, err := client.GetReportJobs(token, report.Id, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("queued %d jobs, want 1", len(jobs))
	}
	if jobs[0].Status != lib.ReportJobPending {
		t.Errorf("status = %q, want %q", jobs[0].Status, lib.ReportJobPending)
	}

	// A scheduled run has to be emailed, which is why the flag exists.
	stored := storedJob(t, jobs[0].Id)
	if !stored.SendEmail {
		t.Error("the queued scheduled job is not marked for emailing")
	}
}

func TestEnqueueDueReportsAdvancesTheSchedule(t *testing.T) {
	requireMongo(t)
	client := testClient(t, &fakeDriver{})
	token := testToken(t, "user-1")

	report, err := client.SaveReportModel(lib.Report{Name: "alle 5 minuten", Cron: "*/5 * * * *"}, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	makeDue(t, report.Id)

	client.enqueueDueReports()

	stored := storedReport(t, report.Id)
	if stored.ScheduledFor == nil {
		t.Fatal("scheduledfor was cleared instead of advanced")
	}
	if !stored.ScheduledFor.After(time.Now()) {
		t.Errorf("scheduledfor = %v, want a date in the future", stored.ScheduledFor)
	}

	// Without the schedule moving forward the report would be queued again on
	// every single tick while it is still rendering.
	client.enqueueDueReports()
	jobs, err := client.GetReportJobs(token, report.Id, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("queued %d jobs over two ticks, want 1", len(jobs))
	}
}

func TestEnqueueDueReportsIgnoresAReportWithoutACron(t *testing.T) {
	requireMongo(t)
	client := testClient(t, &fakeDriver{})
	token := testToken(t, "user-1")

	// A report without a cron has no scheduledfor at all. Mongo compares only
	// within a bson type, so a null must not be picked up by the due query.
	report, err := client.SaveReportModel(lib.Report{Name: "manuell"}, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.ScheduledFor != nil {
		t.Fatalf("scheduledfor = %v, want nil for a report without a cron", report.ScheduledFor)
	}

	client.enqueueDueReports()

	jobs, err := client.GetReportJobs(token, report.Id, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("queued %d jobs for a report without a cron, want 0", len(jobs))
	}
}

func TestEnqueueDueReportsSkipsAReportThatIsStillRunning(t *testing.T) {
	requireMongo(t)
	client := testClient(t, &fakeDriver{})
	token := testToken(t, "user-1")

	report, err := client.SaveReportModel(lib.Report{Name: "langsam", Cron: "*/5 * * * *"}, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	makeDue(t, report.Id)
	client.enqueueDueReports()
	if _, ok := client.claimNextJob(); !ok {
		t.Fatal("the queued job was not claimed")
	}

	makeDue(t, report.Id)
	client.enqueueDueReports()

	jobs, err := client.GetReportJobs(token, report.Id, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("queued %d jobs while one was still running, want 1", len(jobs))
	}
}

func TestEnqueueDueReportsQueuesForTheReportOwner(t *testing.T) {
	requireMongo(t)
	client := testClient(t, &fakeDriver{})

	report, err := client.SaveReportModel(lib.Report{Name: "fremd", Cron: "*/5 * * * *"}, testToken(t, "owner"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	makeDue(t, report.Id)

	client.enqueueDueReports()

	jobs, err := client.GetReportJobs(testToken(t, "owner"), report.Id, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("owner sees %d jobs, want 1", len(jobs))
	}
}

// makeDue backdates the schedule of a report so the next scheduler run picks it up.
func makeDue(t *testing.T, reportId string) {
	t.Helper()
	ctx, cancel := dbCtx()
	defer cancel()
	res, err := Reports().UpdateOne(ctx, bson.M{"_id": reportId},
		bson.M{"$set": bson.M{"scheduledfor": time.Now().Add(-time.Minute)}})
	if err != nil {
		t.Fatalf("could not backdate the schedule: %v", err)
	}
	if res.MatchedCount != 1 {
		t.Fatalf("backdated %d reports, want 1", res.MatchedCount)
	}
}

func storedReport(t *testing.T, reportId string) lib.Report {
	t.Helper()
	ctx, cancel := dbCtx()
	defer cancel()
	var report lib.Report
	if err := Reports().FindOne(ctx, bson.M{"_id": reportId}).Decode(&report); err != nil {
		t.Fatalf("could not read report: %v", err)
	}
	return report
}

// storedJob reads a job including the fields that are not part of the api response.
func storedJob(t *testing.T, jobId string) lib.ReportJob {
	t.Helper()
	ctx, cancel := dbCtx()
	defer cancel()
	var job lib.ReportJob
	if err := ReportJobs().FindOne(ctx, bson.M{"_id": jobId}).Decode(&job); err != nil {
		t.Fatalf("could not read job: %v", err)
	}
	return job
}
