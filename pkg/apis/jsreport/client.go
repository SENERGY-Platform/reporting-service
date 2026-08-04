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
	"net/http"
	"net/url"
	"reflect"
	"strconv"

	"github.com/SENERGY-Platform/reporting-service/lib"
)
import "github.com/go-resty/resty/v2"

type Client struct {
	Url        string
	Port       int64
	BaseUrl    string
	HttpClient *resty.Client
}

var TypeMap = map[string]string{
	"chrome-pdf":   "PDF",
	"xlsx":         "Excel",
	"html-to-xlsx": "Excel",
}

// ErrUnauthorized reports that jsreport did not accept the token it was given.
// jsreport validates the forwarded user token against its configured authorization
// server, so this means that server considered the token invalid — most commonly
// because the introspecting client is not in the audience of the token.
var ErrUnauthorized = fmt.Errorf("jsreport-unauthorized: %w", lib.ErrUnauthorized)

// checkResponse turns a failed answer from jsreport into an error naming the
// status. jsreport answers an unauthenticated request with an empty body, and
// unmarshalling that reports a json error, which hides what actually went wrong.
func checkResponse(response *resty.Response) error {
	if response.IsSuccess() {
		return nil
	}
	if response.StatusCode() == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	var errorResponse ErrorResponse
	if err := json.Unmarshal(response.Body(), &errorResponse); err == nil {
		if errorResponse.Error.Message != "" {
			return errors.New("jsreport-api: " + errorResponse.Message + " - " + errorResponse.Error.Message)
		}
	}
	return fmt.Errorf("jsreport-api: unexpected status %v", response.StatusCode())
}

func NewJSReportClient(url string, port int64) *Client {
	client := resty.New()
	return &Client{Url: url, Port: port, BaseUrl: fmt.Sprintf("%v:%v", url, port), HttpClient: client}
}

func (j *Client) GetTemplates(authString string) (templates []lib.Template, err error) {
	response, err := j.HttpClient.R().SetHeader("Authorization", authString).Get(j.BaseUrl + "/odata/templates?$select=name,recipe")
	if err != nil {
		return
	}
	if err = checkResponse(response); err != nil {
		return
	}
	var resp TemplateResponse
	err = json.Unmarshal(response.Body(), &resp)
	if err != nil {
		return
	}
	for _, jsTemplate := range resp.Templates {
		templates = append(templates, lib.Template{
			Id:   jsTemplate.Id,
			Name: jsTemplate.Name,
			Type: TypeMap[jsTemplate.Recipe],
		})
	}
	return
}

func (j *Client) GetTemplateById(templateId string, authString string) (template lib.Template, err error) {
	response, err := j.HttpClient.R().SetHeader("Authorization", authString).Get(j.BaseUrl + "/odata/templates('" + templateId + "')")
	if err != nil {
		return
	}
	if err = checkResponse(response); err != nil {
		return
	}
	var resp Template
	if err = json.Unmarshal(response.Body(), &resp); err != nil {
		return
	}
	jsData, err := j.getTemplateDataByShortId(resp.Data.ShortId, authString)
	if err != nil {
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

func getJsonKeysAndTypes(jsonData map[string]interface{}) (result map[string]lib.DataType) {
	result = make(map[string]lib.DataType)

	for key, value := range jsonData {
		if _, ok := result[key]; !ok {
			result[key] = lib.DataType{}
		}

		if mapValue, ok := value.(map[string]interface{}); ok { // map
			result[key] = lib.DataType{
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
			result[key] = lib.DataType{
				Name:      key,
				ValueType: "array",
				Length:    len(arrayValue),
				Children:  children,
			}
		} else {
			result[key] = lib.DataType{
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
		return
	}
	if err = checkResponse(response); err != nil {
		return "", "", "", err
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
func (j *Client) GetReportContent(reportId string, authString string) (data []byte, headerContentType string, headerFileExtension string, err error) {
	response, err := j.HttpClient.R().
		SetHeader("Authorization", authString).
		Get(j.BaseUrl + "/reports/" + reportId + "/content")
	if err != nil {
		return
	}
	if err = checkResponse(response); err != nil {
		return nil, "", "", err
	}
	return response.Body(), response.Header().Get("Content-Type"), response.Header().Get("File-Extension"), err
}

func (j *Client) GetTemplatePreview(id string, authString string) (data []byte, headerContentType string, headerFileExtension string, err error) {
	response, err := j.HttpClient.R().
		SetHeader("Authorization", authString).
		SetBody(map[string]interface{}{"template": map[string]interface{}{"_id": id},
			"options": map[string]interface{}{"reports": map[string]interface{}{"save": true, "async": false},
				"reportName": "preview"}}).
		Post(j.BaseUrl + "/api/report")
	if err != nil {
		return
	}
	if err = checkResponse(response); err != nil {
		return nil, "", "", err
	}
	return response.Body(), response.Header().Get("Content-Type"), response.Header().Get("File-Extension"), err
}

func (j *Client) DeleteCreatedReportFile(reportId string, authString string) (err error) {
	response, err := j.HttpClient.R().
		SetHeader("Authorization", authString).
		Delete(j.BaseUrl + "/odata/reports('" + reportId + "')")
	if err != nil {
		return
	}
	if response.StatusCode() != http.StatusNoContent {
		var errorResponse ErrorResponse
		_ = json.Unmarshal(response.Body(), &errorResponse)
		// a file jsreport does not know is already gone, which is what was wanted
		if errorResponse.Error.Message == reportNotFoundErrorMessage(reportId) {
			return nil
		}
		// not errorResponse.Error.Message directly: a rejected request comes back
		// with an empty body, which would produce an error without a message
		return checkResponse(response)
	}
	return
}

func (j *Client) getTemplateDataByShortId(id string, authString string) (data Data, err error) {
	response, err := j.HttpClient.R().SetHeader("Authorization", authString).Get(j.BaseUrl + "/odata/data?$filter=" + url.QueryEscape("shortid eq '"+id+"'"))
	if err != nil {
		return
	}
	if err = checkResponse(response); err != nil {
		return
	}
	var resp DataResponse
	if err = json.Unmarshal(response.Body(), &resp); err != nil {
		return
	}
	if len(resp.Data) > 0 {
		data = resp.Data[0]
	}
	return
}

func reportNotFoundErrorMessage(reportId string) string {
	return fmt.Sprintf("Report %s not found", reportId)
}
