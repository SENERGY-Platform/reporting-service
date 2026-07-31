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

package reportsync

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// tarballServer serves a repository archive the way codeload does: everything
// wrapped in one top level directory.
func tarballServer(t *testing.T, files map[string]string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gzipWriter := gzip.NewWriter(w)
		tarWriter := tar.NewWriter(gzipWriter)
		for name, content := range files {
			_ = tarWriter.WriteHeader(&tar.Header{
				Name:     "report-templates-main/" + name,
				Mode:     0o644,
				Size:     int64(len(content)),
				Typeflag: tar.TypeReg,
			})
			_, _ = tarWriter.Write([]byte(content))
		}
		_ = tarWriter.Close()
		_ = gzipWriter.Close()
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestGitSourceReadsReportFilesFromRepositoryRoot(t *testing.T) {
	host := tarballServer(t, map[string]string{
		"README.md":                           "ignore me",
		"device_state/device-state.script.js": "script from git",
		"device_state/helpers.asset.js":       "asset from git",
		"docs/notes.txt":                      "ignore me too",
	})

	files, err := GitSource{Repo: "SENERGY-Platform/report-templates", Ref: "main", Dir: ".", Host: host}.Files(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("want only the two report files, got %v", files)
	}
	if files["device_state/device-state.script.js"] != "script from git" {
		t.Errorf("script content wrong: %q", files["device_state/device-state.script.js"])
	}
}

func TestGitSourceHonoursSubdirectoryAndReportsEmptyResult(t *testing.T) {
	host := tarballServer(t, map[string]string{
		"reports/device_state/device-state.script.js": "in subdir",
		"other/device_state/device-state.script.js":   "outside",
	})

	files, err := GitSource{Repo: "x/y", Ref: "main", Dir: "reports", Host: host}.Files(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files["device_state/device-state.script.js"] != "in subdir" {
		t.Errorf("want only the subdirectory content, got %v", files)
	}

	_, err = GitSource{Repo: "x/y", Ref: "main", Dir: "nothing-here", Host: host}.Files(context.Background())
	if !errors.Is(err, ErrNothingFound) {
		t.Errorf("want ErrNothingFound, got %v", err)
	}
}
