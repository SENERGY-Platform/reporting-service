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
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxFileSize guards against pulling something unexpected out of an archive.
const maxFileSize = 4 << 20

// GitSource describes where to fetch the report sources from, so an in-cluster
// run can read them straight from a ref instead of from a checkout.
type GitSource struct {
	// Repo is the "owner/name" of the GitHub repository.
	Repo string
	// Ref is a branch, tag or commit.
	Ref string
	// Dir is the directory inside the repository mirroring the managed folder,
	// "." or "" for the repository root.
	Dir string
	// Token is optional, only needed for private repositories.
	Token string
	// Host overrides the public archive host, only used by tests.
	Host string
	// ApiHost overrides the api host used with a token, only used by tests.
	ApiHost string
}

// archiveUrl is the endpoint the archive is fetched from. Without a token the
// public archive host is enough; with one the api endpoint has to be used, which
// answers with a redirect to a signed archive url. codeload does not accept
// bearer tokens.
func (g GitSource) archiveUrl() string {
	if g.Token != "" {
		host := g.ApiHost
		if host == "" {
			host = "https://api.github.com"
		}
		return fmt.Sprintf("%s/repos/%s/tarball/%s", strings.TrimRight(host, "/"), g.Repo, g.Ref)
	}
	host := g.Host
	if host == "" {
		host = "https://codeload.github.com"
	}
	return fmt.Sprintf("%s/%s/tar.gz/%s", strings.TrimRight(host, "/"), g.Repo, g.Ref)
}

// Files fetches the repository archive at the given ref and returns the report
// files of GitSource.Dir keyed by their path relative to it.
func (g GitSource) Files(ctx context.Context) (map[string]string, error) {
	target := g.archiveUrl()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	// the api rejects requests without a user agent
	req.Header.Set("User-Agent", "jsreport-sync")
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	// the redirect target is signed, and http.Client drops the authorization
	// header across hosts anyway
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound && g.Token == "" {
		return nil, fmt.Errorf("could not fetch %s: %s - a private repository needs a token", target, resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("could not fetch %s: %s", target, resp.Status)
	}

	gzipReader, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()

	wanted := strings.Trim(g.Dir, "/")
	if wanted == "." {
		wanted = ""
	}
	if wanted != "" {
		wanted += "/"
	}
	result := map[string]string{}
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		// the archive wraps everything in a "<repo>-<ref>/" directory
		_, relative, found := strings.Cut(header.Name, "/")
		if !found || !strings.HasPrefix(relative, wanted) {
			continue
		}
		if _, _, ok := KindOf(relative); !ok {
			continue
		}
		content, err := io.ReadAll(io.LimitReader(tarReader, maxFileSize+1))
		if err != nil {
			return nil, err
		}
		if len(content) > maxFileSize {
			return nil, fmt.Errorf("%s is larger than %d bytes", header.Name, maxFileSize)
		}
		result[strings.TrimPrefix(relative, wanted)] = string(content)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("%w in %s at ref %s", ErrNothingFound, g.Dir, g.Ref)
	}
	return result, nil
}
