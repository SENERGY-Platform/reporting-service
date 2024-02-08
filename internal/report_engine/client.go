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
	"fmt"
	"report-service/internal/apis/senergy_db_v3"
	"report-service/internal/helper"
)

type Client struct {
	Driver   ReportingDriver
	DBClient *senergy_db_v3.Client
}

// NewClient creates a new client with the given reporting driver.
func NewClient(driver ReportingDriver) *Client {
	dbClient := senergy_db_v3.NewClient(helper.GetEnv("SENERGY_DB_URL", "http://localhost"), helper.GetEnv("SENERGY_DB_PORT", "80"))
	return &Client{Driver: driver, DBClient: dbClient}
}

func (r *Client) GetTemplates() (templates []Template, err error) {
	templates, err = r.Driver.GetTemplates()
	return
}

func (r *Client) GetTemplateById(id string) (template Template, err error) {
	template, err = r.Driver.GetTemplateById(id)
	return
}

func (r *Client) CreateReport(id string, data map[string]ReportObject, authTokenString string) (err error) {
	reportData, err := r.setReportData(data, authTokenString)
	fmt.Println(fmt.Sprintf("%s", reportData))
	err = r.Driver.CreateReport(id, reportData)
	return
}

func (r *Client) setReportData(data map[string]ReportObject, authToken string) (resultData map[string]interface{}, err error) {
	resultData = make(map[string]interface{}, len(data))
	for key, value := range data {
		switch value.ValueType {
		case "string", "int", "float":
			resultData[key] = value.Value
		case "object":
			resultData[key], err = r.setReportData(value.Fields, authToken)
			if err != nil {
				return
			}
		case "array":
			if value.Value != nil {
				resultData[key] = value.Value
			} else if len(value.Children) > 0 {
				resultData[key], err = r.setReportData(value.Children, authToken)
				if err != nil {
					return
				}
			} else if value.Query != nil {
				var responseData []interface{}
				responseData, err = r.DBClient.Query(authToken, *value.Query)
				if err != nil {
					return
				}
				resultData[key] = responseData
			}
		}
	}
	return
}
