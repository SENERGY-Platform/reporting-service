/*
 * Copyright 2024 InfAI (CC SES)
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

package report_engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	timescaleModels "github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	mailpit "github.com/axllent/mailpit/server/apiv1"
)

type Template struct {
	Name string `json:"name,omitempty"`
	Id   string `json:"id,omitempty"`
	Data Data   `json:"data,omitempty"`
	Type string `json:"type,omitempty"`
}

type Data struct {
	Name           string              `json:"name,omitempty"`
	Id             string              `json:"id,omitempty"`
	DataJSONString string              `json:"dataJsonString,omitempty"`
	DataStructured map[string]DataType `json:"dataStructured,omitempty"`
}
type DataType struct {
	Name      string              `json:"name,omitempty"`
	ValueType string              `json:"valueType,omitempty"`
	Length    int                 `json:"length,omitempty"`
	Fields    map[string]DataType `json:"fields,omitempty"`
	Children  map[string]DataType `json:"children,omitempty"`
}

type ReportObject struct {
	DataType
	Value        interface{}                            `json:"value,omitempty"`
	Query        *timescaleModels.QueriesRequestElement `json:"query,omitempty"`
	QueryOptions *QueryOptions                          `json:"queryOptions,omitempty"`
	Fields       map[string]ReportObject                `json:"fields,omitempty"`
	Children     map[string]ReportObject                `json:"children,omitempty"`
}

type QueryOptions struct {
	RollingStartDate string `json:"rollingStartDate,omitempty"`
	RollingEndDate   string `json:"rollingEndDate,omitempty"`
	StartOffset      int    `json:"startOffset,omitempty"`
	EndOffset        int    `json:"endOffset,omitempty"`
}

type Report struct {
	Id             string                  `bson:"_id" json:"id,omitempty"`
	Name           string                  `json:"name,omitempty"`
	TemplateName   string                  `json:"templateName,omitempty"`
	Data           map[string]ReportObject `json:"data,omitempty"`
	TemplateId     string                  `json:"templateId,omitempty"`
	UserId         string                  `json:"userId,omitempty"`
	ReportFiles    []ReportFile            `json:"reportFiles,omitempty"`
	Cron           string                  `json:"cron,omitempty"`
	ScheduledFor   *time.Time              `json:"-"` // internal use
	EmailReceivers []string                `json:"emailReceivers"`
	EmailSubject   string                  `json:"emailSubject,omitempty"`
	EmailText      string                  `json:"emailText,omitempty"`
	EmailHTML      string                  `json:"emailHTML,omitempty"`
	CreatedAt      time.Time               `json:"createdAt,omitempty"`
	UpdatedAt      time.Time               `json:"updatedAt,omitempty"`
}

type ReportFile struct {
	Id        string    `json:"id,omitempty"`
	Link      string    `json:"-"`
	Type      string    `json:"type,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
}

type FromTo = struct {
	Name  string
	Email string
}

type SendRequest mailpit.SendRequest

func (s *SendRequest) Send(remoteAddress string) (messageId string, err error) {
	body, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	req, err := http.NewRequest(http.MethodPost, remoteAddress+"/api/v1/send", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode > 299 {
		return "", fmt.Errorf("unexpected statuscode %v : %v", resp.StatusCode, string(respBody))
	}
	return string(respBody), nil

}
