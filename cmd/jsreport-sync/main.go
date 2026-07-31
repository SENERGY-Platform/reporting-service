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

// jsreport-sync keeps the report entities of one jsreport folder in sync with a
// directory of files. Only the content carrying properties of templates,
// scripts, data entities and assets are written, nothing is ever deleted.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/SENERGY-Platform/reporting-service/pkg/reportsync"
)

const usage = `usage: jsreport-sync <pull|push|diff> [flags]

  pull   write the report entities of the managed folder into the local directory
  push   apply the local files (or a git ref) to the target instance
  diff   report what push would change, exit code 1 on drift

flags:
`

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	command := os.Args[1]

	flags := flag.NewFlagSet(command, flag.ExitOnError)
	flags.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		flags.PrintDefaults()
	}
	url := flags.String("url", env("JSREPORT_URL", "http://localhost:5488"), "jsreport base url")
	user := flags.String("user", env("JSREPORT_USER", ""), "jsreport user for basic auth")
	password := flags.String("password", env("JSREPORT_PASSWORD", ""), "jsreport password for basic auth")
	token := flags.String("token", env("JSREPORT_TOKEN", ""), "bearer token, takes precedence over basic auth")
	folder := flags.String("folder", env("JSREPORT_FOLDER", "/senergy_reports"), "managed jsreport folder")
	dir := flags.String("dir", env("JSREPORT_SYNC_DIR", "."), "local directory mirroring the managed folder")
	create := flags.Bool("create", envBool("JSREPORT_SYNC_CREATE"), "push: insert entities the target does not have yet")
	gitRef := flags.String("from-git", env("JSREPORT_SYNC_REF", ""), "push: read the files from this git ref instead of -dir")
	gitRepo := flags.String("repo", env("JSREPORT_SYNC_REPO", "SENERGY-Platform/report-templates"), "repository for -from-git")
	gitToken := flags.String("repo-token", env("JSREPORT_SYNC_REPO_TOKEN", ""), "token for -from-git, only needed for private repositories")
	timeout := flags.Duration("timeout", 2*time.Minute, "timeout of the whole run")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return 2
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, *timeout)
	defer cancelTimeout()

	client := reportsync.NewClient(reportsync.ClientConfig{
		BaseUrl:  *url,
		User:     *user,
		Password: *password,
		Token:    *token,
	})

	dryRun := command == "diff"
	syncer := reportsync.New(client, reportsync.Options{
		Folder: *folder,
		Dir:    *dir,
		Create: *create,
		DryRun: dryRun,
	})

	var changes []reportsync.Change
	var err error
	switch command {
	case "pull":
		changes, err = syncer.Pull(ctx)
	case "push", "diff":
		var source map[string]string
		if *gitRef != "" {
			source, err = reportsync.GitSource{
				Repo:  *gitRepo,
				Ref:   *gitRef,
				Dir:   *dir,
				Token: *gitToken,
			}.Files(ctx)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
		}
		changes, err = syncer.Push(ctx, source)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", command)
		fmt.Fprint(os.Stderr, usage)
		flags.PrintDefaults()
		return 2
	}

	report(command, *folder, changes, dryRun, *create)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if command == "diff" && reportsync.Drift(changes) {
		return 1
	}
	return 0
}

func report(command string, folder string, changes []reportsync.Change, dryRun bool, create bool) {
	if len(changes) == 0 {
		return
	}
	counts := map[reportsync.Action]int{}
	width := 0
	for _, change := range changes {
		counts[change.Action]++
		if len(change.Path) > width {
			width = len(change.Path)
		}
	}
	for _, change := range changes {
		line := fmt.Sprintf("%-11s %-*s %s", change.Action, width, change.Path, change.Kind)
		if change.Bytes != 0 {
			line += fmt.Sprintf(" %+d bytes", change.Bytes)
		}
		switch {
		case change.Action == reportsync.ActionInvalid:
			line += " (" + change.Reason + ", not written)"
		case change.Action == reportsync.ActionRemoteOnly && command == "pull":
			line += " (only local, not in " + folder + ")"
		case change.Action == reportsync.ActionRemoteOnly:
			line += " (only in " + folder + ", left untouched)"
		case change.Action == reportsync.ActionCreate && command != "pull" && !change.Kind.Creatable:
			line += " (missing in target, create it in the studio)"
		case change.Action == reportsync.ActionCreate && command != "pull" && !create:
			line += " (missing in target, use -create)"
		}
		fmt.Println(strings.TrimRight(line, " "))
	}

	summary := make([]string, 0, len(counts))
	for _, action := range []reportsync.Action{
		reportsync.ActionUpdate,
		reportsync.ActionCreate,
		reportsync.ActionInvalid,
		reportsync.ActionUnchanged,
		reportsync.ActionRemoteOnly,
	} {
		if counts[action] > 0 {
			summary = append(summary, fmt.Sprintf("%d %s", counts[action], action))
		}
	}
	suffix := ""
	if dryRun {
		suffix = " (dry run)"
	}
	fmt.Printf("%s %s: %s%s\n", command, folder, strings.Join(summary, ", "), suffix)
}

func env(key string, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func envBool(key string) bool {
	value := strings.ToLower(os.Getenv(key))
	return value == "true" || value == "1" || value == "yes"
}
