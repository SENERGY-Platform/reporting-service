/*
 * Copyright 2025 InfAI (CC SES)
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

package lib

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	timescaleModels "github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
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
	DeviceQuery  *DeviceQuery                           `json:"deviceQuery,omitempty"`
	Fields       map[string]ReportObject                `json:"fields,omitempty"`
	Children     map[string]ReportObject                `json:"children,omitempty"`
}

type QueryOptions struct {
	RollingStartDate *string `json:"rollingStartDate,omitempty"`
	RollingEndDate   *string `json:"rollingEndDate,omitempty"`
	StartOffset      *int    `json:"startOffset,omitempty"`
	EndOffset        *int    `json:"endOffset,omitempty"`
	ResultObject     *string `json:"resultObject,omitempty"`
	ResultKey        *int    `json:"resultKey,omitempty"`
}

type DeviceQuery struct {
	Last *string `json:"last,omitempty"`
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

type SendRequest struct {
	// "From" recipient
	// required: true
	From struct {
		// Optional name
		// example: John Doe
		Name string
		// Email address
		// example: john@example.com
		// required: true
		Email string
	}

	// "To" recipients
	To []struct {
		// Optional name
		// example: Jane Doe
		Name string
		// Email address
		// example: jane@example.com
		// required: true
		Email string
	}

	// Cc recipients
	Cc []struct {
		// Optional name
		// example: Manager
		Name string
		// Email address
		// example: manager@example.com
		// required: true
		Email string
	}

	// Bcc recipients email addresses only
	// example: ["jack@example.com"]
	Bcc []string

	// Optional Reply-To recipients
	ReplyTo []struct {
		// Optional name
		// example: Secretary
		Name string
		// Email address
		// example: secretary@example.com
		// required: true
		Email string
	}

	// Subject
	// example: Mailpit message via the HTTP API
	Subject string

	// Message body (text)
	// example: Mailpit is awesome!
	Text string

	// Message body (HTML)
	// example: <div style="text-align:center"><p style="font-family: arial; font-size: 24px;">Mailpit is <b>awesome</b>!</p><p><img src="cid:mailpit-logo" /></p></div>
	HTML string

	// Attachments
	Attachments []struct {
		// Base64-encoded string of the file content
		// required: true
		// example: iVBORw0KGgoAAAANSUhEUgAAAEEAAAA8CAMAAAAOlSdoAAAACXBIWXMAAAHrAAAB6wGM2bZBAAAAS1BMVEVHcEwRfnUkZ2gAt4UsSF8At4UtSV4At4YsSV4At4YsSV8At4YsSV4At4YsSV4sSV4At4YsSV4At4YtSV4At4YsSV4At4YtSV8At4YsUWYNAAAAGHRSTlMAAwoXGiktRE5dbnd7kpOlr7zJ0d3h8PD8PCSRAAACWUlEQVR42pXT4ZaqIBSG4W9rhqQYocG+/ys9Y0Z0Br+x3j8zaxUPewFh65K+7yrIMeIY4MT3wPfEJCidKXEMnLaVkxDiELiMz4WEOAZSFghxBIypCOlKiAMgXfIqTnBgSm8CIQ6BImxEUxEckClVQiHGj4Ba4AQHikAIClwTE9KtIghAhUJwoLkmLnCiAHJLRKgIMsEtVUKbBUIwoAg2C4QgQBE6l4VCnApBgSKYLLApCnCa0+96AEMW2BQcmC+Pr3nfp7o5Exy49gIADcIqUELGfeA+bp93LmAJp8QJoEcN3C7NY3sbVANixMyI0nku20/n5/ZRf3KI2k6JEDWQtxcbdGuAqu3TAXG+/799Oyyas1B1MnMiA+XyxHp9q0PUKGPiRAau1fZbLRZV09wZcT8/gHk8QQAxXn8VgaDqcUmU6O/r28nbVwXAqca2mRNtPAF5+zoP2MeN9Fy4NgC6RfcbgE7XITBRYTtOE3U3C2DVff7pk+PkUxgAbvtnPXJaD6DxulMLwOhPS/M3MQkgg1ZFrIXnmfaZoOfpKiFgzeZD/WuKqQEGrfJYkyWf6vlG3xUgTuscnkNkQsb599q124kdpMUjCa/XARHs1gZymVtGt3wLkiFv8rUgTxitYCex5EVGec0Y9VmoDTFBSQte2TfXGXlf7hbdaUM9Sk7fisEN9qfBBTK+FZcvM9fQSdkl2vj4W2oX/bRogO3XasiNH7R0eW7fgRM834ImTg+Lg6BEnx4vz81rhr+MYPBBQg1v8GndEOrthxaCTxNAOut8WKLGZQl+MPz88Q9tAO/hVuSeqQAAAABJRU5ErkJggg==
		Content string
		// Filename
		// required: true
		// example: mailpit.png
		Filename string
		// Optional Content Type for the the attachment.
		// If this field is not set (or empty) then the content type is automatically detected.
		// required: false
		// example: image/png
		ContentType string
		// Optional Content-ID (`cid`) for attachment.
		// If this field is set then the file is attached inline.
		// required: false
		// example: mailpit-logo
		ContentID string
	}

	// Mailpit tags
	// example: ["Tag 1","Tag 2"]
	Tags []string

	// Optional headers in {"key":"value"} format
	// example: {"X-IP":"1.2.3.4"}
	Headers map[string]string
}

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
