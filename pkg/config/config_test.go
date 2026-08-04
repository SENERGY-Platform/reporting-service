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

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewAppliesDefaults(t *testing.T) {
	cfg, err := New("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ServerPort != 8080 {
		t.Errorf("server port = %d, want 8080", cfg.ServerPort)
	}
	if cfg.MongoDatabase != "reporting" {
		t.Errorf("mongo database = %q, want %q", cfg.MongoDatabase, "reporting")
	}
	if cfg.ReportJobWorkers != 2 {
		t.Errorf("report job workers = %d, want 2", cfg.ReportJobWorkers)
	}
}

// Every duration in the config is parsed at runtime, so a default that does not
// parse would only blow up when the service starts.
func TestDefaultDurationsParse(t *testing.T) {
	cfg, err := New("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	durations := map[string]string{
		"scheduler_ticker_duration": cfg.SchedulerTickerDuration,
		"report_job_retention":      cfg.ReportJobRetention,
		"report_job_stale_after":    cfg.ReportJobStaleAfter,
	}
	for name, value := range durations {
		if _, err = time.ParseDuration(value); err != nil {
			t.Errorf("%s = %q does not parse: %v", name, value, err)
		}
	}
}

// The stale timeout has to stay above the heartbeat interval of the workers,
// otherwise a healthy job would be reaped underneath its own worker.
func TestDefaultStaleTimeoutExceedsTheHeartbeatInterval(t *testing.T) {
	cfg, err := New("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	staleAfter, err := time.ParseDuration(cfg.ReportJobStaleAfter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if staleAfter <= 15*time.Second {
		t.Errorf("report_job_stale_after = %v, want more than the 15s heartbeat interval", staleAfter)
	}
}

func TestNewReadsAConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{"server_port": 9090, "mongo_database": "custom", "report_job_workers": 5}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("could not write config: %v", err)
	}

	cfg, err := New(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ServerPort != 9090 {
		t.Errorf("server port = %d, want 9090", cfg.ServerPort)
	}
	if cfg.MongoDatabase != "custom" {
		t.Errorf("mongo database = %q, want %q", cfg.MongoDatabase, "custom")
	}
	if cfg.ReportJobWorkers != 5 {
		t.Errorf("report job workers = %d, want 5", cfg.ReportJobWorkers)
	}
	// values not mentioned in the file keep their default
	if cfg.ReportJobStaleAfter != "2m" {
		t.Errorf("report_job_stale_after = %q, want the default %q", cfg.ReportJobStaleAfter, "2m")
	}
}

func TestNewPrefersTheEnvironmentOverTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"report_job_workers": 5}`), 0o600); err != nil {
		t.Fatalf("could not write config: %v", err)
	}
	t.Setenv("REPORT_JOB_WORKERS", "7")
	t.Setenv("MONGODB_DATABASE", "from-env")

	cfg, err := New(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ReportJobWorkers != 7 {
		t.Errorf("report job workers = %d, want 7 from the environment", cfg.ReportJobWorkers)
	}
	if cfg.MongoDatabase != "from-env" {
		t.Errorf("mongo database = %q, want %q", cfg.MongoDatabase, "from-env")
	}
}

func TestNewReportsAMissingConfigFile(t *testing.T) {
	if _, err := New(filepath.Join(t.TempDir(), "does-not-exist.json")); err == nil {
		t.Error("got no error for a config path that does not exist, want one")
	}
}
