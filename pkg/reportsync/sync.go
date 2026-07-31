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
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type Action string

const (
	// ActionUnchanged means the target already holds the wanted value.
	ActionUnchanged Action = "unchanged"
	// ActionUpdate means the property of an existing entity gets replaced.
	ActionUpdate Action = "update"
	// ActionCreate means the entity does not exist in the target yet.
	ActionCreate Action = "create"
	// ActionRemoteOnly means the target has a value the source does not know
	// about. Those are only reported, never deleted.
	ActionRemoteOnly Action = "remote-only"
	// ActionInvalid means the source file was rejected before writing.
	ActionInvalid Action = "invalid"
)

// Change is one planned or executed step of a sync run.
type Change struct {
	Action Action
	Kind   Kind
	// Path is the absolute jsreport path of the entity.
	Path string
	// File is the source file relative to the managed folder.
	File string
	// Id is the id of the existing target entity, empty for ActionCreate.
	Id string
	// Bytes is the size difference, negative when the source is shorter.
	Bytes int
	// Reason explains ActionInvalid.
	Reason string
}

type Options struct {
	// Folder is the managed jsreport folder, e.g. "/senergy_reports". Nothing
	// outside of it is ever read or written.
	Folder string
	// Dir is the local directory mirroring the managed folder.
	Dir string
	// Create allows inserting entities the target does not have yet.
	Create bool
	// DryRun plans the changes without writing anything.
	DryRun bool
}

type Syncer struct {
	client *Client
	opts   Options
}

func New(client *Client, opts Options) *Syncer {
	opts.Folder = "/" + strings.Trim(opts.Folder, "/")
	if opts.Dir == "" {
		opts.Dir = "."
	}
	return &Syncer{client: client, opts: opts}
}

// record is one syncable property of one entity in the target.
type record struct {
	kind   Kind
	path   string
	file   string
	id     string
	value  string
	folder string
}

// Pull writes the report entities of the managed folder into the local directory.
func (s *Syncer) Pull(ctx context.Context) ([]Change, error) {
	remote, _, err := s.remote(ctx)
	if err != nil {
		return nil, err
	}
	if len(remote) == 0 {
		return nil, fmt.Errorf("%w in %s", ErrNothingFound, s.opts.Folder)
	}

	source, err := s.localFiles()
	if err != nil {
		return nil, err
	}

	var changes []Change
	for _, file := range sortedKeys(remote) {
		entry := remote[file]
		change := Change{Kind: entry.kind, Path: entry.path, File: file, Id: entry.id}
		current, exists := source[file]
		switch {
		case exists && current == entry.value:
			change.Action = ActionUnchanged
		case exists:
			change.Action = ActionUpdate
			change.Bytes = len(entry.value) - len(current)
		default:
			change.Action = ActionCreate
			change.Bytes = len(entry.value)
		}
		if change.Action != ActionUnchanged && !s.opts.DryRun {
			target := filepath.Join(s.opts.Dir, filepath.FromSlash(file))
			if err = os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return changes, err
			}
			if err = os.WriteFile(target, []byte(entry.value), 0o644); err != nil {
				return changes, err
			}
		}
		changes = append(changes, change)
	}

	// files without a counterpart in the instance, e.g. left over after a rename
	for _, file := range sortedKeys(source) {
		if _, ok := remote[file]; !ok {
			kind, name, _ := KindOf(file)
			changes = append(changes, Change{
				Action: ActionRemoteOnly,
				Kind:   kind,
				File:   file,
				Path:   s.opts.Folder + "/" + path.Join(path.Dir(file), name),
			})
		}
	}
	return changes, nil
}

// Push applies the source to the target. Only the properties listed in Kinds are
// written, nothing is deleted.
func (s *Syncer) Push(ctx context.Context, source map[string]string) ([]Change, error) {
	if source == nil {
		var err error
		source, err = s.localFiles()
		if err != nil {
			return nil, err
		}
	}
	if len(source) == 0 {
		return nil, fmt.Errorf("%w in %s", ErrNothingFound, s.opts.Dir)
	}

	remote, folderPaths, err := s.remote(ctx)
	if err != nil {
		return nil, err
	}

	var changes []Change
	for _, file := range sortedKeys(source) {
		content := source[file]
		kind, name, ok := KindOf(file)
		if !ok {
			continue
		}
		entityPath := s.opts.Folder + "/" + path.Join(path.Dir(file), name)
		change := Change{Kind: kind, Path: entityPath, File: file}

		if kind.Validate != nil {
			if err = kind.Validate(content); err != nil {
				change.Action = ActionInvalid
				change.Reason = err.Error()
				changes = append(changes, change)
				continue
			}
		}

		entry, exists := remote[file]
		switch {
		case exists && entry.value == content:
			change.Action = ActionUnchanged
			change.Id = entry.id
		case exists:
			change.Action = ActionUpdate
			change.Id = entry.id
			change.Bytes = len(content) - len(entry.value)
			if !s.opts.DryRun {
				if err = s.client.PatchProperty(ctx, kind.Collection, entry.id, kind.Property, kind.encode(content)); err != nil {
					return changes, fmt.Errorf("could not update %s of %s: %w", kind, change.Path, err)
				}
			}
		default:
			change.Action = ActionCreate
			change.Bytes = len(content)
			if !s.opts.Create || !kind.Creatable {
				changes = append(changes, change)
				continue
			}
			folderShortid, found := folderShortidOf(entityPath, folderPaths)
			if !found {
				return changes, fmt.Errorf("cannot create %s: folder %s does not exist in the target", change.Path, path.Dir(entityPath))
			}
			if !s.opts.DryRun {
				if err = s.client.Create(ctx, kind.Collection, name, folderShortid, kind.Property, kind.encode(content)); err != nil {
					return changes, fmt.Errorf("could not create %s: %w", change.Path, err)
				}
			}
		}
		changes = append(changes, change)
	}

	for _, file := range sortedKeys(remote) {
		if _, ok := source[file]; !ok {
			entry := remote[file]
			changes = append(changes, Change{
				Action: ActionRemoteOnly,
				Kind:   entry.kind,
				File:   file,
				Path:   entry.path,
				Id:     entry.id,
			})
		}
	}
	return changes, nil
}

// remote collects the syncable properties of the managed folder, keyed by the
// file they belong in.
func (s *Syncer) remote(ctx context.Context) (map[string]record, map[string]string, error) {
	folders, err := s.client.Folders(ctx)
	if err != nil {
		return nil, nil, err
	}
	folderPaths, err := FolderPaths(folders)
	if err != nil {
		return nil, nil, err
	}

	prefix := s.opts.Folder + "/"
	result := map[string]record{}
	for collection, properties := range Collections() {
		entities, err := s.client.List(ctx, collection, properties)
		if err != nil {
			return nil, nil, err
		}
		for _, entity := range entities {
			entityPath, err := EntityPath(entity, folderPaths)
			if err != nil {
				return nil, nil, err
			}
			if !strings.HasPrefix(entityPath, prefix) {
				continue
			}
			for _, property := range properties {
				kind, ok := kindFor(collection, property)
				if !ok {
					continue
				}
				raw := entity.Properties[property]
				if raw == "" {
					// an unset property has no file, e.g. a template without helpers
					continue
				}
				value, err := kind.decode(raw)
				if err != nil {
					return nil, nil, err
				}
				relative := strings.TrimPrefix(entityPath, prefix)
				file := path.Join(path.Dir(relative), kind.FileName(entity.Name))
				result[file] = record{
					kind:   kind,
					path:   entityPath,
					file:   file,
					id:     entity.Id,
					value:  value,
					folder: entity.FolderRef,
				}
			}
		}
	}
	return result, folderPaths, nil
}

// localFiles reads the mirror directory, keyed by the path relative to it.
func (s *Syncer) localFiles() (map[string]string, error) {
	result := map[string]string{}
	info, err := os.Stat(s.opts.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", s.opts.Dir)
	}
	err = filepath.WalkDir(s.opts.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if _, _, ok := KindOf(d.Name()); !ok {
			return nil
		}
		content, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(s.opts.Dir, p)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(relative)] = string(content)
		return nil
	})
	return result, err
}

func folderShortidOf(entityPath string, folderPaths map[string]string) (string, bool) {
	parent := path.Dir(entityPath)
	for shortid, folderPath := range folderPaths {
		if folderPath == parent {
			return shortid, true
		}
	}
	return "", false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Drift reports whether changes would modify the target.
func Drift(changes []Change) bool {
	for _, change := range changes {
		if change.Action == ActionUpdate || change.Action == ActionCreate || change.Action == ActionInvalid {
			return true
		}
	}
	return false
}
