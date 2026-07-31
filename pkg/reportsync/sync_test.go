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
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type write struct {
	collection string
	id         string
	property   string
	value      string
}

// stub serves the collections a sync run reads and records every write.
type stub struct {
	collections map[string][]map[string]any
	patches     []write
	inserts     []write
}

func newStub() *stub {
	return &stub{collections: map[string][]map[string]any{
		"folders": {
			{"_id": "f1", "name": "senergy_reports", "shortid": "root"},
			{"_id": "f2", "name": "device_state", "shortid": "sub", "folder": map[string]any{"shortid": "root"}},
			{"_id": "f3", "name": "elsewhere", "shortid": "other"},
		},
		"templates": {
			{"_id": "t1", "name": "device_state", "shortid": "tp1", "folder": map[string]any{"shortid": "sub"},
				"content": "<html>old</html>", "helpers": "function a(){}"},
			{"_id": "t2", "name": "outside", "shortid": "tp2", "folder": map[string]any{"shortid": "other"},
				"content": "keep"},
		},
		"scripts": {
			{"_id": "s1", "name": "device-state.js", "shortid": "sc1", "folder": map[string]any{"shortid": "sub"},
				"content": "script old"},
		},
		"data": {
			{"_id": "d1", "name": "device-state-data", "shortid": "da1", "folder": map[string]any{"shortid": "sub"},
				"dataJson": `{"end":"old"}`},
		},
		"assets": {
			{"_id": "a1", "name": "helpers.js", "shortid": "as1", "folder": map[string]any{"shortid": "sub"},
				"content": base64.StdEncoding.EncodeToString([]byte("function isDefined(){}"))},
		},
	}}
}

func (s *stub) client(t *testing.T) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		collection := strings.TrimPrefix(r.URL.Path, "/odata/")
		switch r.Method {
		case http.MethodGet:
			entities, ok := s.collections[collection]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"value": entities})
		case http.MethodPatch:
			name, id, _ := strings.Cut(collection, "(")
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			for property, value := range body {
				s.patches = append(s.patches, write{name, strings.Trim(id, "')"), property, value})
			}
			w.WriteHeader(http.StatusNoContent)
		case http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			for property, value := range body {
				if property == "name" || property == "folder" {
					continue
				}
				text, _ := value.(string)
				s.inserts = append(s.inserts, write{collection, stringOf(body["name"]), property, text})
			}
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(server.Close)
	return NewClient(ClientConfig{BaseUrl: server.URL})
}

func (s *stub) syncer(t *testing.T, opts Options) *Syncer {
	t.Helper()
	if opts.Folder == "" {
		opts.Folder = "/senergy_reports"
	}
	return New(s.client(t), opts)
}

func actionsByFile(changes []Change) map[string]Action {
	result := map[string]Action{}
	for _, change := range changes {
		result[change.File] = change.Action
	}
	return result
}

func TestPullWritesEveryKindOfTheManagedFolder(t *testing.T) {
	s := newStub()
	dir := t.TempDir()

	if _, err := s.syncer(t, Options{Dir: dir}).Pull(context.Background()); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"device_state/device_state.template.handlebars": "<html>old</html>",
		"device_state/device_state.template.helpers.js": "function a(){}",
		"device_state/device-state.script.js":           "script old",
		"device_state/device-state-data.data.json":      `{"end":"old"}`,
		"device_state/helpers.asset.js":                 "function isDefined(){}",
	}
	for file, content := range want {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file)))
		if err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}
		if string(got) != content {
			t.Errorf("%s: want %q, got %q", file, content, string(got))
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "outside.template.handlebars")); !os.IsNotExist(err) {
		t.Error("entities outside the managed folder must not be written")
	}
}

func TestPushWritesOnlyChangedPropertiesAndDecodesAssets(t *testing.T) {
	s := newStub()
	dir := t.TempDir()
	writeFile(t, dir, "device_state/device_state.template.handlebars", "<html>new</html>")
	writeFile(t, dir, "device_state/device_state.template.helpers.js", "function a(){}")
	writeFile(t, dir, "device_state/helpers.asset.js", "function isDefined(value){}")

	changes, err := s.syncer(t, Options{Dir: dir}).Push(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	actions := actionsByFile(changes)
	if actions["device_state/device_state.template.handlebars"] != ActionUpdate {
		t.Error("changed template content must be an update")
	}
	if actions["device_state/device_state.template.helpers.js"] != ActionUnchanged {
		t.Error("identical helpers must be unchanged")
	}
	if len(s.patches) != 2 {
		t.Fatalf("want 2 patches, got %v", s.patches)
	}
	byProperty := map[string]write{}
	for _, patch := range s.patches {
		byProperty[patch.collection+"."+patch.property] = patch
	}
	if got := byProperty["templates.content"]; got.id != "t1" || got.value != "<html>new</html>" {
		t.Errorf("template content patch wrong: %+v", got)
	}
	asset := byProperty["assets.content"]
	decoded, err := base64.StdEncoding.DecodeString(asset.value)
	if err != nil {
		t.Fatalf("asset content must be base64: %v", err)
	}
	if string(decoded) != "function isDefined(value){}" {
		t.Errorf("asset content wrong: %q", string(decoded))
	}
}

func TestPushRejectsInvalidDataJsonBeforeWriting(t *testing.T) {
	s := newStub()
	dir := t.TempDir()
	writeFile(t, dir, "device_state/device-state-data.data.json", "{not json")

	changes, err := s.syncer(t, Options{Dir: dir}).Push(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if actionsByFile(changes)["device_state/device-state-data.data.json"] != ActionInvalid {
		t.Error("broken json must be reported as invalid")
	}
	if len(s.patches) != 0 {
		t.Errorf("invalid source must not be written, got %v", s.patches)
	}
	if !Drift(changes) {
		t.Error("invalid counts as drift, so a check fails")
	}
}

func TestPushNeverCreatesTemplatesButCreatesScriptsWithFlag(t *testing.T) {
	s := newStub()
	dir := t.TempDir()
	writeFile(t, dir, "device_state/fresh.template.handlebars", "<html/>")
	writeFile(t, dir, "device_state/fresh.script.js", "fresh")

	changes, err := s.syncer(t, Options{Dir: dir, Create: true}).Push(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	actions := actionsByFile(changes)
	if actions["device_state/fresh.template.handlebars"] != ActionCreate {
		t.Error("missing template must be planned as create")
	}
	if len(s.inserts) != 1 {
		t.Fatalf("want exactly one insert, got %v", s.inserts)
	}
	if s.inserts[0].collection != "scripts" || s.inserts[0].id != "fresh.js" {
		t.Errorf("only the script may be inserted, got %+v", s.inserts[0])
	}
}

func TestPushLeavesRemoteOnlyEntitiesAlone(t *testing.T) {
	s := newStub()
	dir := t.TempDir()
	writeFile(t, dir, "device_state/device-state.script.js", "script old")

	changes, err := s.syncer(t, Options{Dir: dir}).Push(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	actions := actionsByFile(changes)
	for _, file := range []string{
		"device_state/device_state.template.handlebars",
		"device_state/device-state-data.data.json",
		"device_state/helpers.asset.js",
	} {
		if actions[file] != ActionRemoteOnly {
			t.Errorf("%s: want remote-only, got %q", file, actions[file])
		}
	}
	if len(s.patches) != 0 || len(s.inserts) != 0 {
		t.Error("remote-only entities must not be touched")
	}
	if Drift(changes) {
		t.Error("remote-only alone is not drift")
	}
}

func TestDryRunPlansWithoutWriting(t *testing.T) {
	s := newStub()
	dir := t.TempDir()
	writeFile(t, dir, "device_state/device-state.script.js", "changed")

	changes, err := s.syncer(t, Options{Dir: dir, DryRun: true}).Push(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !Drift(changes) {
		t.Error("want drift reported")
	}
	if len(s.patches) != 0 {
		t.Errorf("dry run must not patch, got %v", s.patches)
	}
}

func TestKindOfMapsFileNames(t *testing.T) {
	cases := []struct {
		file       string
		collection string
		property   string
		name       string
	}{
		{"device_state.template.handlebars", "templates", "content", "device_state"},
		{"device_state.template.helpers.js", "templates", "helpers", "device_state"},
		{"device-state.script.js", "scripts", "content", "device-state.js"},
		{"device-state-data.data.json", "data", "dataJson", "device-state-data"},
		{"helpers.asset.js", "assets", "content", "helpers.js"},
		{"logo.asset.png", "assets", "content", "logo.png"},
		{"plain.asset", "assets", "content", "plain"},
	}
	for _, c := range cases {
		kind, name, ok := KindOf(c.file)
		if !ok {
			t.Errorf("%s: not recognized", c.file)
			continue
		}
		if kind.Collection != c.collection || kind.Property != c.property || name != c.name {
			t.Errorf("%s: got %s/%s name %q, want %s/%s name %q",
				c.file, kind.Collection, kind.Property, name, c.collection, c.property, c.name)
		}
	}
	for _, file := range []string{"README.md", "notes.txt", "device-state.js"} {
		if _, _, ok := KindOf(file); ok {
			t.Errorf("%s must not be recognized as a report file", file)
		}
	}
}

func TestFileNameRoundTrip(t *testing.T) {
	for _, kind := range Kinds {
		for _, name := range []string{"device_state", "helpers.js", "some-name"} {
			file := kind.FileName(name)
			gotKind, gotName, ok := KindOf(file)
			if !ok {
				t.Errorf("%s of %s: not recognized", file, kind)
				continue
			}
			if gotKind.Collection != kind.Collection || gotKind.Property != kind.Property || gotName != name {
				t.Errorf("%s: got %s name %q, want %s name %q", file, gotKind, gotName, kind, name)
			}
		}
	}
}

func TestFolderPathsDetectsMissingParent(t *testing.T) {
	_, err := FolderPaths([]Entity{{Name: "child", Shortid: "c", FolderRef: "gone"}})
	if err == nil {
		t.Fatal("want an error for a missing parent folder")
	}
}

func writeFile(t *testing.T, dir string, name string, content string) {
	t.Helper()
	target := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
