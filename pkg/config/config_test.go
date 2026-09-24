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

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sb_util "github.com/SENERGY-Platform/go-service-base/util"
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
	t.Setenv("MONGO_DATABASE", "from-env")

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

var mongoEnv = []string{"MONGO_URL", "MONGO_USER", "MONGO_PASSWORD", "MONGO_AUTH_SOURCE", "MONGO_DATABASE", "MONGODB_URI", "MONGODB_DATABASE"}

// clearMongoEnv unsets the variables for this test; t.Setenv restores them afterwards.
func clearMongoEnv(t *testing.T) {
	t.Helper()
	for _, k := range mongoEnv {
		t.Setenv(k, "")
		if err := os.Unsetenv(k); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNewMongoDefaults(t *testing.T) {
	clearMongoEnv(t)
	cfg, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MongoUrl != "mongodb://localhost:27017" {
		t.Errorf("mongo url = %q", cfg.MongoUrl)
	}
	if cfg.MongoUser != "" || cfg.MongoPassword != "" {
		t.Errorf("mongo user = %q, password set = %v, want both empty", cfg.MongoUser, cfg.MongoPassword != "")
	}
	if cfg.MongoAuthSource != "admin" {
		t.Errorf("mongo auth source = %q, want admin", cfg.MongoAuthSource)
	}
	if cfg.MongoDatabase != "reporting" {
		t.Errorf("mongo database = %q, want reporting", cfg.MongoDatabase)
	}
}

func TestNewMongoEnvNames(t *testing.T) {
	clearMongoEnv(t)
	t.Setenv("MONGO_URL", "mongodb://mongo-0:27017/?replicaSet=rs0")
	t.Setenv("MONGO_USER", "reporting-service")
	t.Setenv("MONGO_PASSWORD", "s3cr3t")
	t.Setenv("MONGO_AUTH_SOURCE", "users")
	t.Setenv("MONGO_DATABASE", "other_db")
	cfg, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MongoUrl != "mongodb://mongo-0:27017/?replicaSet=rs0" || cfg.MongoUser != "reporting-service" ||
		cfg.MongoPassword.Value() != "s3cr3t" || cfg.MongoAuthSource != "users" || cfg.MongoDatabase != "other_db" {
		t.Errorf("mongo config not taken from the environment: url %q, user %q, auth source %q, database %q",
			cfg.MongoUrl, cfg.MongoUser, cfg.MongoAuthSource, cfg.MongoDatabase)
	}
}

// The deployment switches names together with the image, so the old ones must not linger.
func TestNewIgnoresOldMongoEnvNames(t *testing.T) {
	clearMongoEnv(t)
	t.Setenv("MONGODB_URI", "mongodb://db.reporting:27017")
	t.Setenv("MONGODB_DATABASE", "old")
	cfg, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MongoUrl != "mongodb://localhost:27017" || cfg.MongoDatabase != "reporting" {
		t.Errorf("old names still read: url %q, database %q", cfg.MongoUrl, cfg.MongoDatabase)
	}
}

// The loader overwrites a default with "" when the variable is set but empty,
// which is why InitDB rejects an empty database name.
func TestNewEmptyMongoDatabaseEnvOverridesTheDefault(t *testing.T) {
	clearMongoEnv(t)
	t.Setenv("MONGO_DATABASE", "")
	cfg, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MongoDatabase != "" {
		t.Errorf("mongo database = %q, want empty", cfg.MongoDatabase)
	}
}

func TestNewReadsMongoCredentialsFromAConfigFile(t *testing.T) {
	clearMongoEnv(t)
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{"mongo_url": "mongodb://file:27017", "mongo_user": "u", "mongo_password": "s3cr3t", "mongo_auth_source": "a", "mongo_database": "d"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MongoUrl != "mongodb://file:27017" || cfg.MongoUser != "u" || cfg.MongoPassword.Value() != "s3cr3t" ||
		cfg.MongoAuthSource != "a" || cfg.MongoDatabase != "d" {
		t.Errorf("mongo config not taken from the file: url %q, user %q, auth source %q, database %q",
			cfg.MongoUrl, cfg.MongoUser, cfg.MongoAuthSource, cfg.MongoDatabase)
	}
}

// main logs the whole config as JSON at startup.
func TestConfigJSONMasksTheMongoPassword(t *testing.T) {
	clearMongoEnv(t)
	t.Setenv("MONGO_USER", "reporting-service")
	t.Setenv("MONGO_PASSWORD", "s3cr3t")
	cfg, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	if s := sb_util.ToJsonStr(cfg); strings.Contains(s, "s3cr3t") {
		t.Errorf("config JSON leaks the password: %s", s)
	}
}
