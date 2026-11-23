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

package figures

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// StatusError is a non-200 answer of the figures service.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("figures.client - response code %d: %s", e.Status, e.Body)
}

type Client struct {
	BaseUrl    string
	HttpClient *resty.Client
}

// NewClient takes the url of the service including the gateway's path for it.
func NewClient(baseUrl string, timeout time.Duration) *Client {
	return &Client{BaseUrl: strings.TrimRight(baseUrl, "/"), HttpClient: resty.New().SetTimeout(timeout)}
}

type Range struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type TotalsRequest struct {
	GraphId  string `json:"graphId"`
	Range    Range  `json:"range"`
	TimeZone string `json:"timeZone"`
	Routing  string `json:"routing,omitempty"`
	Net      bool   `json:"net"`
}

type Position struct {
	At    string  `json:"at"`
	Value float64 `json:"value"`
}

// MeterReading is one register. An end that was not read is absent, a feed-in
// register may come with its last end only.
type MeterReading struct {
	Carrier   string    `json:"carrier,omitempty"`
	Direction string    `json:"direction,omitempty"`
	Measure   string    `json:"measure,omitempty"`
	First     *Position `json:"first,omitempty"`
	Last      *Position `json:"last,omitempty"`
}

// TotalsResponse holds the part of the answer to POST /consumption/totals that is
// read here.
type TotalsResponse struct {
	GraphId          string                         `json:"graphId"`
	Range            Range                          `json:"range"`
	ReadingsByDevice map[string][]MeterReading      `json:"readingsByDevice"`
	Totals           map[string]map[string]*float64 `json:"totals"`
}

// Totals totals the consumption per node of a graph with the caller's token.
func (c *Client) Totals(authTokenString string, request TotalsRequest) (data TotalsResponse, err error) {
	response, err := c.HttpClient.R().
		SetHeader("Authorization", authTokenString).
		SetBody(request).
		Post(c.BaseUrl + "/consumption/totals")
	if err != nil {
		return
	}
	if response.StatusCode() != http.StatusOK {
		return data, &StatusError{Status: response.StatusCode(), Body: response.String()}
	}
	if err = json.Unmarshal(response.Body(), &data); err != nil {
		return data, fmt.Errorf("figures.client - response unmarshal error: %w", err)
	}
	return
}
