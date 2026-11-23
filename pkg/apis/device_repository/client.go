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

package device_repository

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// StatusError is a non-200 answer of the device repository.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("device_repository.client - response code %d: %s", e.Status, e.Body)
}

type Client struct {
	Url        string
	Port       int64
	BaseUrl    string
	HttpClient *resty.Client
}

func NewClient(url string, port int64, timeout time.Duration) *Client {
	client := resty.New().SetTimeout(timeout)
	return &Client{Url: url, Port: port, BaseUrl: fmt.Sprintf("%v:%v", url, port), HttpClient: client}
}

type Attribute struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type GraphNode struct {
	Id string `json:"id"`
	// ResourceType is device, dashboard-custom, thing or empty for group nodes.
	ResourceType string      `json:"resource_type"`
	ResourceId   string      `json:"resource_id"`
	Attributes   []Attribute `json:"attributes"`
}

// GraphEdge points from the child to its parent. A weight of zero means detached,
// an absent weight is an edge whose share is not known.
type GraphEdge struct {
	FromNodeId string   `json:"from_node_id"`
	ToNodeId   string   `json:"to_node_id"`
	Weight     *float64 `json:"weight"`
}

type Graph struct {
	Id         string      `json:"id"`
	Attributes []Attribute `json:"attributes"`
	Nodes      []GraphNode `json:"nodes"`
	Edges      []GraphEdge `json:"edges"`
}

// GetGraph returns a unit graph the token may read.
func (c *Client) GetGraph(authTokenString string, id string) (graph Graph, err error) {
	response, err := c.HttpClient.R().
		SetHeader("Authorization", authTokenString).
		Get(c.BaseUrl + "/device-repository/graphs/" + url.PathEscape(id))
	if err != nil {
		return
	}
	if response.StatusCode() != http.StatusOK {
		return graph, &StatusError{Status: response.StatusCode(), Body: response.String()}
	}
	if err = json.Unmarshal(response.Body(), &graph); err != nil {
		return graph, fmt.Errorf("device_repository.client - response unmarshal error: %w", err)
	}
	return
}

const deviceTypeChunk = 50

// GetDeviceTypeNames reads the device type name of each device it can read, keyed by
// device id. A device the token may not read, or whose type has no name, is absent.
func (c *Client) GetDeviceTypeNames(authTokenString string, deviceIds []string) (map[string]string, error) {
	names := map[string]string{}
	for start := 0; start < len(deviceIds); start += deviceTypeChunk {
		chunk := deviceIds[start:min(start+deviceTypeChunk, len(deviceIds))]
		escaped := make([]string, len(chunk))
		for i, id := range chunk {
			escaped[i] = url.QueryEscape(id)
		}
		response, err := c.HttpClient.R().
			SetHeader("Authorization", authTokenString).
			Get(c.BaseUrl + "/device-repository/extended-devices?fulldt=false&ids=" + strings.Join(escaped, ","))
		if err != nil {
			return nil, err
		}
		if response.StatusCode() != http.StatusOK {
			return nil, &StatusError{Status: response.StatusCode(), Body: response.String()}
		}
		var devices []struct {
			Id             string `json:"id"`
			DeviceTypeName string `json:"device_type_name"`
		}
		if err = json.Unmarshal(response.Body(), &devices); err != nil {
			return nil, fmt.Errorf("device_repository.client - response unmarshal error: %w", err)
		}
		for _, device := range devices {
			if device.Id != "" && device.DeviceTypeName != "" {
				names[device.Id] = device.DeviceTypeName
			}
		}
	}
	return names, nil
}
