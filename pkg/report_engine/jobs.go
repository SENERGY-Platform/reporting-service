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
	"sync"
	"time"

	"github.com/SENERGY-Platform/reporting-service/lib"
	"github.com/SENERGY-Platform/reporting-service/pkg/util"
	"github.com/SENERGY-Platform/service-commons/pkg/jwt"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	// jobHeartbeatInterval has to stay well below jobStaleAfter, otherwise a
	// healthy worker would have its own job reaped underneath it.
	jobHeartbeatInterval = 15 * time.Second
	// jobPollInterval only bounds how long a queued job can wait when the notify
	// channel was missed; the common case is picked up immediately.
	jobPollInterval = 5 * time.Second
	// DefaultReportJobLimit is the number of jobs returned when a client does not
	// ask for a specific amount.
	DefaultReportJobLimit int64 = 20
	// MaxReportJobLimit caps how many jobs a single request can ask for.
	MaxReportJobLimit int64 = 100
)

// ErrJobNotFound is returned when a report job does not exist or belongs to a
// different user.
var ErrJobNotFound = errors.New("report job not found")

// jobQueue holds the state shared between the api handlers and the workers. The
// Client is copied by value into every gin handler, so this has to be reached
// through a pointer for the mutex to protect anything.
type jobQueue struct {
	notify   chan struct{}
	mu       sync.Mutex
	inFlight map[string]struct{}
}

func newJobQueue() *jobQueue {
	return &jobQueue{
		notify:   make(chan struct{}, 1),
		inFlight: map[string]struct{}{},
	}
}

// EnqueueReportFileCreation queues creation of a report file and returns the id of
// the report it belongs to together with the id of the job tracking it. The report
// model is stored here rather than in the worker, so the caller can be handed a
// report id right away.
func (r *Client) EnqueueReportFileCreation(reportRequest lib.Report, authTokenString string) (reportId string, jobId string, err error) {
	claims, err := jwt.Parse(authTokenString)
	if err != nil {
		return
	}
	report, err := r.ensureReportModel(reportRequest, authTokenString)
	if err != nil {
		return
	}
	job, err := r.insertReportJob(lib.ReportJob{
		ReportId: report.Id,
		UserId:   claims.GetUserId(),
		Request:  report,
	})
	if err != nil {
		return
	}
	util.Logger.Info("queued report file creation", "report_id", report.Id, "job_id", job.Id)
	return report.Id, job.Id, nil
}

// GetReportJob returns a single job of the calling user. It returns ErrJobNotFound
// for both an unknown job and a job of another user, so the endpoint does not
// disclose which of the two it was.
func (r *Client) GetReportJob(jobId string, authTokenString string) (job lib.ReportJob, err error) {
	claims, err := jwt.Parse(authTokenString)
	if err != nil {
		return
	}
	ctx, cancel := dbCtx()
	defer cancel()
	err = ReportJobs().FindOne(ctx, bson.M{"_id": jobId, "userid": claims.GetUserId()}).Decode(&job)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return lib.ReportJob{}, ErrJobNotFound
	}
	if err != nil {
		return lib.ReportJob{}, err
	}
	return job, nil
}

// GetReportJobs returns the newest jobs of the calling user, optionally limited to
// a single report.
func (r *Client) GetReportJobs(authTokenString string, reportId string, limit int64) (jobs []lib.ReportJob, err error) {
	claims, err := jwt.Parse(authTokenString)
	if err != nil {
		return
	}
	if limit <= 0 {
		limit = DefaultReportJobLimit
	}
	if limit > MaxReportJobLimit {
		limit = MaxReportJobLimit
	}
	req := bson.M{"userid": claims.GetUserId()}
	if reportId != "" {
		req["reportid"] = reportId
	}
	ctx, cancel := dbCtx()
	defer cancel()
	cur, err := ReportJobs().Find(ctx, req, options.Find().
		SetSort(bson.D{{Key: "createdat", Value: -1}}).
		SetLimit(limit))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close(ctx) }()
	// A polling client is easier to write against an empty list than against null.
	jobs = []lib.ReportJob{}
	for cur.Next(ctx) {
		var job lib.ReportJob
		if err = cur.Decode(&job); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, cur.Err()
}

// RunJobWorkers processes queued report jobs until ctx is done. Jobs are claimed
// from the database instead of an in-memory queue, so whatever was still waiting
// is picked up again after a restart. The method blocks.
func (r *Client) RunJobWorkers(ctx context.Context) error {
	staleAfter, err := time.ParseDuration(r.Config.ReportJobStaleAfter)
	if err != nil {
		return errors.New("invalid report_job_stale_after: " + err.Error())
	}
	if staleAfter <= jobHeartbeatInterval {
		return errors.New("report_job_stale_after has to be longer than " + jobHeartbeatInterval.String())
	}
	workers := r.Config.ReportJobWorkers
	if workers < 1 {
		workers = 1
	}
	util.Logger.Info("init report job workers", "workers", workers)
	wg := &sync.WaitGroup{}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.workJobs(ctx, staleAfter)
		}()
	}
	wg.Wait()
	// Renders that are still running are abandoned rather than waited for, so a
	// long report cannot hold up shutdown past the pod termination grace period.
	r.failInFlightJobs("report creation was interrupted by a service restart")
	return ctx.Err()
}

func (r *Client) workJobs(ctx context.Context, staleAfter time.Duration) {
	ticker := time.NewTicker(jobPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			util.Logger.Info("report job worker received shutdown signal")
			return
		case <-r.jobs.notify:
		case <-ticker.C:
			r.reapStaleJobs(staleAfter)
		}
		for ctx.Err() == nil {
			job, ok := r.claimNextJob()
			if !ok {
				break
			}
			if !r.runJob(ctx, job) {
				// ctx was cancelled while the job was still running
				return
			}
		}
	}
}

// claimNextJob moves the oldest pending job to running in a single atomic update,
// so that concurrent workers can never pick up the same job.
func (r *Client) claimNextJob() (job lib.ReportJob, ok bool) {
	ctx, cancel := dbCtx()
	defer cancel()
	now := time.Now()
	err := ReportJobs().FindOneAndUpdate(
		ctx,
		bson.M{"status": lib.ReportJobPending},
		bson.M{"$set": bson.M{
			"status":    lib.ReportJobRunning,
			"step":      lib.ReportJobStepCollectingData,
			"startedat": now,
			"heartbeat": now,
		}},
		options.FindOneAndUpdate().
			SetSort(bson.D{{Key: "createdat", Value: 1}}).
			SetReturnDocument(options.After),
	).Decode(&job)
	if err != nil {
		if !errors.Is(err, mongo.ErrNoDocuments) {
			util.Logger.Error("could not claim report job", "error", err)
		}
		return lib.ReportJob{}, false
	}
	r.jobs.mu.Lock()
	r.jobs.inFlight[job.Id] = struct{}{}
	r.jobs.mu.Unlock()
	return job, true
}

// runJob renders the report file of a claimed job. It reports whether the job ran
// to completion; false means ctx was cancelled while the render was still going,
// in which case the render is left to die with the process.
func (r *Client) runJob(ctx context.Context, job lib.ReportJob) (completed bool) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.executeJob(ctx, job)
	}()
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}

func (r *Client) executeJob(ctx context.Context, job lib.ReportJob) {
	defer r.releaseJob(job.Id)
	stopHeartbeat := r.startHeartbeat(ctx, job.Id)
	defer stopHeartbeat()

	util.Logger.Info("creating report file", "job_id", job.Id, "report_id", job.ReportId)

	// A fresh token is exchanged instead of carrying the one from the request, so
	// a long running job cannot fail halfway through on an expired token.
	token, _, err := jwt.ExchangeUserToken(
		r.Config.Keycloak.Url,
		r.Config.Keycloak.ClientId,
		r.Config.Keycloak.ClientSecret,
		job.UserId,
	)
	if err != nil {
		util.Logger.Error("could not exchange user token", "error", err, "job_id", job.Id)
		r.finishJob(job.Id, "", errors.New("could not authenticate for report creation"))
		return
	}

	report, reportFileId, err := r.CreateReportFileWithProgress(job.Request, token.Token, func(step string) {
		r.setJobStep(job.Id, step)
	})
	if err != nil {
		util.Logger.Error("could not create report file", "error", err, "job_id", job.Id)
		r.finishJob(job.Id, "", err)
		return
	}

	if job.SendEmail {
		r.setJobStep(job.Id, lib.ReportJobStepEmailing)
		if _, err = r.EmailReport(reportFileId, report, token.Token); err != nil {
			util.Logger.Error("could not email report", "error", err, "job_id", job.Id)
			r.finishJob(job.Id, reportFileId, err)
			return
		}
	}
	r.finishJob(job.Id, reportFileId, nil)
	util.Logger.Info("created report file", "job_id", job.Id, "report_id", report.Id, "report_file_id", reportFileId)
}

func (r *Client) insertReportJob(job lib.ReportJob) (lib.ReportJob, error) {
	job.Id = uuid.New().String()
	job.Status = lib.ReportJobPending
	job.CreatedAt = time.Now()
	ctx, cancel := dbCtx()
	defer cancel()
	if _, err := ReportJobs().InsertOne(ctx, job); err != nil {
		return lib.ReportJob{}, err
	}
	r.notifyJobWorkers()
	return job, nil
}

// hasUnfinishedJob reports whether a report is already queued or being rendered.
func (r *Client) hasUnfinishedJob(reportId string) (bool, error) {
	ctx, cancel := dbCtx()
	defer cancel()
	count, err := ReportJobs().CountDocuments(ctx, bson.M{
		"reportid": reportId,
		"status":   bson.M{"$in": []lib.ReportJobStatus{lib.ReportJobPending, lib.ReportJobRunning}},
	}, options.Count().SetLimit(1))
	return count > 0, err
}

func (r *Client) notifyJobWorkers() {
	if r.jobs == nil {
		return
	}
	select {
	case r.jobs.notify <- struct{}{}:
	default: // a worker is already on its way to look for work
	}
}

func (r *Client) startHeartbeat(ctx context.Context, jobId string) (stop func()) {
	hbCtx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(jobHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				r.updateRunningJob(jobId, bson.M{"heartbeat": time.Now()})
			}
		}
	}()
	return cancel
}

func (r *Client) setJobStep(jobId string, step string) {
	r.updateRunningJob(jobId, bson.M{"step": step, "heartbeat": time.Now()})
}

// finishJob records the outcome of a job.
func (r *Client) finishJob(jobId string, reportFileId string, jobErr error) {
	now := time.Now()
	update := bson.M{
		"status":       lib.ReportJobDone,
		"reportfileid": reportFileId,
		"step":         "",
		"finishedat":   now,
		"heartbeat":    now,
	}
	if jobErr != nil {
		update["status"] = lib.ReportJobFailed
		update["error"] = jobErr.Error()
	}
	r.updateRunningJob(jobId, update)
}

// updateRunningJob writes to a job only while it is still running. That guard is
// what keeps a job abandoned during shutdown from being resurrected by the render
// that outlived the worker, whichever of the two writes lands first.
func (r *Client) updateRunningJob(jobId string, fields bson.M) {
	ctx, cancel := dbCtx()
	defer cancel()
	_, err := ReportJobs().UpdateOne(ctx,
		bson.M{"_id": jobId, "status": lib.ReportJobRunning},
		bson.M{"$set": fields})
	if err != nil {
		util.Logger.Error("could not update report job", "error", err, "job_id", jobId)
	}
}

// reapStaleJobs fails jobs whose worker stopped writing heartbeats, which is what
// a kill during a render looks like afterwards. They are deliberately not retried,
// because such a job may well have produced a report file before it died.
func (r *Client) reapStaleJobs(staleAfter time.Duration) {
	ctx, cancel := dbCtx()
	defer cancel()
	now := time.Now()
	res, err := ReportJobs().UpdateMany(ctx,
		bson.M{
			"status":    lib.ReportJobRunning,
			"heartbeat": bson.M{"$lt": now.Add(-staleAfter)},
		},
		bson.M{"$set": bson.M{
			"status":     lib.ReportJobFailed,
			"error":      "report creation was interrupted",
			"step":       "",
			"finishedat": now,
		}})
	if err != nil {
		util.Logger.Error("could not reap stale report jobs", "error", err)
		return
	}
	if res.ModifiedCount > 0 {
		util.Logger.Warn("failed stale report jobs", "count", res.ModifiedCount)
	}
}

func (r *Client) failInFlightJobs(reason string) {
	if r.jobs == nil {
		return
	}
	r.jobs.mu.Lock()
	ids := make([]string, 0, len(r.jobs.inFlight))
	for id := range r.jobs.inFlight {
		ids = append(ids, id)
	}
	r.jobs.mu.Unlock()
	for _, id := range ids {
		r.finishJob(id, "", errors.New(reason))
	}
}

func (r *Client) releaseJob(jobId string) {
	r.jobs.mu.Lock()
	delete(r.jobs.inFlight, jobId)
	r.jobs.mu.Unlock()
}
