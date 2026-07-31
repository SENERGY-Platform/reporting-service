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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Entity is one jsreport entity, reduced to what the sync needs. Properties
// holds the syncable values of the entity, keyed by property name.
type Entity struct {
	Id         string
	Name       string
	Shortid    string
	FolderRef  string
	Properties map[string]string
}

type Client struct {
	baseUrl    string
	user       string
	password   string
	token      string
	httpClient *http.Client
}

type ClientConfig struct {
	BaseUrl  string
	User     string
	Password string
	Token    string
	Timeout  time.Duration
}

func NewClient(conf ClientConfig) *Client {
	timeout := conf.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseUrl:    strings.TrimRight(conf.BaseUrl, "/"),
		user:       conf.User,
		password:   conf.Password,
		token:      conf.Token,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// Folders returns all folder entities, needed to resolve entity paths.
func (c *Client) Folders(ctx context.Context) ([]Entity, error) {
	return c.List(ctx, "folders", nil)
}

// List returns the entities of a collection including the given properties.
func (c *Client) List(ctx context.Context, collection string, properties []string) ([]Entity, error) {
	fields := append([]string{"_id", "name", "shortid", "folder"}, properties...)
	target := c.baseUrl + "/odata/" + collection + "?$select=" + url.QueryEscape(strings.Join(fields, ","))
	req, err := c.newRequest(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	body, err := c.do(req)
	if err != nil {
		return nil, err
	}
	var response struct {
		Value []map[string]any `json:"value"`
	}
	if err = json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("could not parse %s response: %w", collection, err)
	}

	entities := make([]Entity, 0, len(response.Value))
	for _, raw := range response.Value {
		entity := Entity{
			Id:         stringOf(raw["_id"]),
			Name:       stringOf(raw["name"]),
			Shortid:    stringOf(raw["shortid"]),
			Properties: map[string]string{},
		}
		if folder, ok := raw["folder"].(map[string]any); ok {
			entity.FolderRef = stringOf(folder["shortid"])
		}
		for _, property := range properties {
			if value, ok := raw[property]; ok {
				entity.Properties[property] = stringOf(value)
			}
		}
		entities = append(entities, entity)
	}
	return entities, nil
}

// PatchProperty updates a single property of a single entity. Nothing else of
// the entity is sent, so ids, links and permissions stay as they are.
func (c *Client) PatchProperty(ctx context.Context, collection string, id string, property string, value string) error {
	body, err := json.Marshal(map[string]string{property: value})
	if err != nil {
		return err
	}
	target := fmt.Sprintf("%s/odata/%s('%s')", c.baseUrl, collection, url.PathEscape(id))
	req, err := c.newRequest(ctx, http.MethodPatch, target, body)
	if err != nil {
		return err
	}
	_, err = c.do(req)
	return err
}

// Create inserts a new entity into the given folder. Links to other entities are
// not created, that stays a manual step in the studio.
func (c *Client) Create(ctx context.Context, collection string, name string, folderShortid string, property string, value string) error {
	entity := map[string]any{"name": name, property: value}
	if folderShortid != "" {
		entity["folder"] = map[string]string{"shortid": folderShortid}
	}
	body, err := json.Marshal(entity)
	if err != nil {
		return err
	}
	req, err := c.newRequest(ctx, http.MethodPost, c.baseUrl+"/odata/"+collection, body)
	if err != nil {
		return err
	}
	_, err = c.do(req)
	return err
}

func (c *Client) newRequest(ctx context.Context, method string, target string, body []byte) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	switch {
	case c.token != "":
		req.Header.Set("Authorization", "Bearer "+c.token)
	case c.user != "":
		req.SetBasicAuth(c.user, c.password)
	}
	return req, nil
}

func (c *Client) do(req *http.Request) ([]byte, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("jsreport rejected the credentials (%s)", resp.Status)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("jsreport answered %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func stringOf(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

// FolderPaths maps every folder shortid to its absolute path, e.g. "/senergy_reports/device_state".
func FolderPaths(folders []Entity) (map[string]string, error) {
	byShortid := make(map[string]Entity, len(folders))
	for _, folder := range folders {
		byShortid[folder.Shortid] = folder
	}
	paths := make(map[string]string, len(folders))
	for _, folder := range folders {
		path, err := folderPath(folder, byShortid, 0)
		if err != nil {
			return nil, err
		}
		paths[folder.Shortid] = path
	}
	return paths, nil
}

func folderPath(folder Entity, byShortid map[string]Entity, depth int) (string, error) {
	if depth > len(byShortid) {
		return "", fmt.Errorf("folder %q is part of a reference cycle", folder.Name)
	}
	if folder.FolderRef == "" {
		return "/" + folder.Name, nil
	}
	parent, ok := byShortid[folder.FolderRef]
	if !ok {
		return "", fmt.Errorf("parent folder %q of %q does not exist", folder.FolderRef, folder.Name)
	}
	parentPath, err := folderPath(parent, byShortid, depth+1)
	if err != nil {
		return "", err
	}
	return parentPath + "/" + folder.Name, nil
}

// EntityPath returns the absolute jsreport path of an entity.
func EntityPath(entity Entity, folderPaths map[string]string) (string, error) {
	if entity.FolderRef == "" {
		return "/" + entity.Name, nil
	}
	parent, ok := folderPaths[entity.FolderRef]
	if !ok {
		return "", fmt.Errorf("folder %q of entity %q does not exist", entity.FolderRef, entity.Name)
	}
	return parent + "/" + entity.Name, nil
}

var ErrNothingFound = errors.New("no report entities found")
