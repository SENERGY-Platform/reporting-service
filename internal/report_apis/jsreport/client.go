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
	"report-service/internal/report_engine"
)
import "github.com/go-resty/resty/v2"

type Server struct {
	Url        string
	Port       string
	BaseUrl    string
	HttpClient *resty.Client
}

func NewJSReportServer(url string, port string) *Server {
	client := resty.New()
	return &Server{Url: url, Port: port, BaseUrl: fmt.Sprintf("http://%v:%v", url, port), HttpClient: client}
}

func (j *Server) GetTemplates() (templates []report_engine.Template, err error) {
	response, err := j.HttpClient.R().Get(j.BaseUrl + "/odata/templates?$select=name")
	var resp TemplateResponse
	err = json.Unmarshal(response.Body(), &resp)
	for _, jsTemplate := range resp.Templates {
		templates = append(templates, report_engine.Template{Id: jsTemplate.Id, Name: jsTemplate.Name})

	}
	return
}

func (j *Server) GetTemplateById(templateId string) (template report_engine.Template, err error) {
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
	template.Data.DataJSON = jsData.DataJSON
	return
}

func (j *Server) getTemplateDataByShortId(id string) (data Data, err error) {
	response, err := j.HttpClient.R().Get(j.BaseUrl + "/odata/data?$filter=" + url.QueryEscape("shortid eq '"+id+"'"))
	var resp DataResponse
	err = json.Unmarshal(response.Body(), &resp)
	data = resp.Data[0]
	return
}
