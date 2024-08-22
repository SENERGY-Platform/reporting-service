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

type TemplateResponse struct {
	Context   string     `json:"@odata.context,omitempty"`
	Templates []Template `json:"value,omitempty"`
}

type Template struct {
	Name    string  `json:"name,omitempty"`
	Id      string  `json:"_id,omitempty"`
	ShortId string  `json:"shortid,omitempty"`
	Data    ShortId `json:"data,omitempty"`
}

type TemplateOptions struct {
	Reports Reports `json:"reports,omitempty"`
}

type Reports struct {
	Save       bool   `json:"save,omitempty"`
	ReportName string `json:"reportName,omitempty"`
}

type DataResponse struct {
	Context string `json:"@odata.context,omitempty"`
	Data    []Data `json:"value,omitempty"`
}

type Data struct {
	Name     string `json:"name,omitempty"`
	Id       string `json:"_id,omitempty"`
	DataJSON string `json:"dataJson,omitempty"`
}

type ShortId struct {
	ShortId string `json:"shortid,omitempty"`
}

type ErrorResponse struct {
	Message string `json:"message,omitempty"`
	Stack   string `json:"stack,omitempty"`
}
