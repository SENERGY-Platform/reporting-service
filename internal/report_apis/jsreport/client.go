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
	"fmt"
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

func NewJSReportClient(url string, port string) *Client {
	client := resty.New()
	return &Client{Url: url, Port: port, BaseUrl: fmt.Sprintf("http://%v:%v", url, port), HttpClient: client}
}

func (j *Client) GetTemplates() (templates []report_engine.Template, err error) {
	response, err := j.HttpClient.R().Get(j.BaseUrl + "/odata/templates?$select=name")
	var resp TemplateResponse
	err = json.Unmarshal(response.Body(), &resp)
	for _, jsTemplate := range resp.Templates {
		templates = append(templates, report_engine.Template{Id: jsTemplate.Id, Name: jsTemplate.Name})

	}
	return
}

func (j *Client) GetTemplateById(templateId string) (template report_engine.Template, err error) {
	response, err := j.HttpClient.R().Get(j.BaseUrl + "/odata/templates('" + templateId + "')")
	var resp Template
	err = json.Unmarshal(response.Body(), &resp)
	jsData, err := j.getTemplateDataByShortId(resp.Data.ShortId)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	template.Id = resp.Id
	template.Name = resp.Name
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
			getJsonKeysAndTypes(mapValue)
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

func (j *Client) getTemplateDataByShortId(id string) (data Data, err error) {
	response, err := j.HttpClient.R().Get(j.BaseUrl + "/odata/data?$filter=" + url.QueryEscape("shortid eq '"+id+"'"))
	var resp DataResponse
	err = json.Unmarshal(response.Body(), &resp)
	data = resp.Data[0]
	return
}
