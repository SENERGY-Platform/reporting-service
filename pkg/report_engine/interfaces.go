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

import "github.com/SENERGY-Platform/reporting-service/pkg/models"

type ReportingDriver interface {
	GetTemplates(string) ([]models.Template, error)
	GetTemplateById(string, string) (models.Template, error)
	// CreateReport creates a report with the given ID and data.
	//
	// Parameters:
	// - reportName: The name of the report to create.
	// - templateName: The name of the template to use for the report.
	// - data: A map of report objects, which will be used to fill the report template.
	// - authString: The authentication token string.
	//
	// Returns:
	// - reportId: The ID of the created report.
	// - reportType: The type of the created report.
	// - reportLink: A link to the created report.
	// - err: An error if the operation fails.
	CreateReport(reportName string, templateName string, data map[string]interface{}, authString string) (reportId string, reportType string, reportLink string, err error)
	// GetReportContent retrieves the content of the report with the given ID.
	//
	// Parameters:
	// - reportId: The ID of the report to retrieve.
	// - authString: The authentication token string.
	//
	// Returns:
	// - data: The content of the report.
	// - headerContentType: The content type of the report.
	// - headerFileExtension: The file type extension of the report.
	// - err: An error if the retrieval fails.
	GetReportContent(reportId string, authString string) (data []byte, headerContentType string, headerFileExtension string, err error)
	DeleteCreatedReportFile(reportId string, authString string) (err error)
	GetTemplatePreview(id string, authString string) (data []byte, headerContentType string, headerFileExtension string, err error)
}
