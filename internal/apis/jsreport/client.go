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

package jsreport

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"reflect"
	"report-service/internal/report_engine"
	"strconv"
)
import "github.com/go-resty/resty/v2"

type Client struct {
	Url        string
	Port       string
	BaseUrl    string
	HttpClient *resty.Client
}

var TypeMap = map[string]string{
	"chrome-pdf": "PDF",
	"xlsx":       "Excel",
}

func NewJSReportClient(url string, port string) *Client {
	client := resty.New()
	return &Client{Url: url, Port: port, BaseUrl: fmt.Sprintf("%v:%v", url, port), HttpClient: client}
}

func (j *Client) GetTemplates(authString string) (templates []report_engine.Template, err error) {
	response, err := j.HttpClient.R().SetHeader("Authorization", authString).Get(j.BaseUrl + "/odata/templates?$select=name,recipe")
	var resp TemplateResponse
	err = json.Unmarshal(response.Body(), &resp)
	for _, jsTemplate := range resp.Templates {
		templates = append(templates, report_engine.Template{
			Id:   jsTemplate.Id,
			Name: jsTemplate.Name,
			Type: TypeMap[jsTemplate.Recipe],
		})
	}
	return
}

func (j *Client) GetTemplateById(templateId string, authString string) (template report_engine.Template, err error) {
	response, err := j.HttpClient.R().SetHeader("Authorization", authString).Get(j.BaseUrl + "/odata/templates('" + templateId + "')")
	var resp Template
	err = json.Unmarshal(response.Body(), &resp)
	jsData, err := j.getTemplateDataByShortId(resp.Data.ShortId, authString)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	template.Id = resp.Id
	template.Name = resp.Name
	template.Type = TypeMap[resp.Recipe]

	template.Data.Id = jsData.Id
	template.Data.Name = jsData.Name
	template.Data.DataJSONString = jsData.DataJSON

	var rawJson map[string]interface{}
	err = json.Unmarshal([]byte(jsData.DataJSON), &rawJson)
	if err != nil {
		return
	}
	template.Data.DataStructured = getJsonKeysAndTypes(rawJson)
	return
}

func getJsonKeysAndTypes(jsonData map[string]interface{}) (result map[string]report_engine.DataType) {
	result = make(map[string]report_engine.DataType)

	for key, value := range jsonData {
		if _, ok := result[key]; !ok {
			result[key] = report_engine.DataType{}
		}

		if mapValue, ok := value.(map[string]interface{}); ok { // map
			result[key] = report_engine.DataType{
				Name:      key,
				ValueType: "object",
				Fields:    getJsonKeysAndTypes(mapValue),
			}
		} else if arrayValue, ok := value.([]interface{}); ok { // array
			childrenMap := make(map[string]interface{})
			for i := 0; i < len(arrayValue); i++ {
				childrenMap[strconv.Itoa(i)] = arrayValue[i]
			}
			children := getJsonKeysAndTypes(childrenMap)
			result[key] = report_engine.DataType{
				Name:      key,
				ValueType: "array",
				Length:    len(arrayValue),
				Children:  children,
			}
		} else {
			result[key] = report_engine.DataType{
				Name:      key,
				ValueType: fmt.Sprintf("%v", reflect.TypeOf(value)),
			}
		}
	}
	return
}

// CreateReport creates a report with the given name, template name, and data.
//
// Parameters:
// - reportName: The name of the report to create. If empty, defaults to "report".
// - templateName: The name of the template to use.
// - data: A map of report data.
// - authString: The authentication token string.
//
// Returns:
// - reportId: The ID of the created report.
// - reportType: The type of the created report.
// - reportLink: The permanent link of the created report.
// - err: An error if the creation fails.
func (j *Client) CreateReport(reportName string, templateName string, data map[string]interface{}, authString string) (reportId string, reportType string, reportLink string, err error) {
	if reportName == "" {
		reportName = "report"
	}
	response, err := j.HttpClient.R().
		SetHeader("Authorization", authString).
		SetBody(map[string]interface{}{
			"template": map[string]interface{}{"name": templateName},
			"options": map[string]interface{}{
				"reports":    map[string]interface{}{"save": true, "async": false},
				"reportName": reportName,
			},
			"data": data}).
		Post(j.BaseUrl + "/api/report")
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	if response.StatusCode() != http.StatusOK {
		if response.StatusCode() == http.StatusUnauthorized {
			return "", "", "", errors.New("jsreport-unauthorized")
		}
		var errorResponse ErrorResponse
		err = json.Unmarshal(response.Body(), &errorResponse)
		return "", "", "", errors.New(errorResponse.Error.Message)
	}
	reportLink = response.Header().Get("Permanent-Link")
	reportType = response.Header().Get("Content-Type")
	reportId = response.Header().Get("Report-Id")
	return
}

// GetReportContent retrieves the content of the report with the given ID.
//
// Parameters:
// - reportId: The ID of the report to retrieve.
// - authString: The authentication token string.
//
// Returns:
// - data: The content of the report.
// - headerContentType: The content type of the report.
// - err: An error if the retrieval fails.
func (j *Client) GetReportContent(reportId string, authString string) (data []byte, headerContentType string, err error) {
	response, err := j.HttpClient.R().
		SetHeader("Authorization", authString).
		Get(j.BaseUrl + "/reports/" + reportId + "/content")
	if err != nil {
		return
	}
	return response.Body(), response.Header().Get("Content-Type"), err
}

func (j *Client) DeleteCreatedReportFile(reportId string, authString string) (err error) {
	response, err := j.HttpClient.R().
		SetHeader("Authorization", authString).
		Delete(j.BaseUrl + "/odata/reports('" + reportId + "')")
	if err != nil {
		log.Println(err.Error())
		return
	}
	if response.StatusCode() != http.StatusNoContent {
		var errorResponse ErrorResponse
		_ = json.Unmarshal(response.Body(), &errorResponse)
		if errorResponse.Error.Message == reportNotFoundErrorMessage(reportId) {
			return
		}
		err = errors.New(errorResponse.Error.Message)
	}
	return
}

func (j *Client) getTemplateDataByShortId(id string, authString string) (data Data, err error) {
	response, err := j.HttpClient.R().SetHeader("Authorization", authString).Get(j.BaseUrl + "/odata/data?$filter=" + url.QueryEscape("shortid eq '"+id+"'"))
	var resp DataResponse
	err = json.Unmarshal(response.Body(), &resp)
	data = resp.Data[0]
	return
}

func reportNotFoundErrorMessage(reportId string) string {
	return fmt.Sprintf("Report %s not found", reportId)
}
