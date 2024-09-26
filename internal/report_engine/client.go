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
	"errors"
	"fmt"
	"log"
	"report-service/internal/apis/senergy_db_v3"
	"report-service/internal/helper"
	"strconv"
	"strings"

	"github.com/SENERGY-Platform/service-commons/pkg/jwt"
	"github.com/globalsign/mgo/bson"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Client struct {
	Driver   ReportingDriver
	DBClient *senergy_db_v3.Client
}

// NewClient creates a new client with the given reporting driver.
//
// Parameters:
// - driver: The reporting driver to use.
//
// Returns:
// - client: The newly created client.
func NewClient(driver ReportingDriver) *Client {
	dbClient := senergy_db_v3.NewClient(
		helper.GetEnv("SENERGY_DB_URL", "http://localhost"),
		helper.GetEnv("SENERGY_DB_PORT", "80"),
	)
	return &Client{Driver: driver, DBClient: dbClient}
}

// GetTemplates retrieves a list of available report templates.
//
// Returns a slice of Template objects and an error if the operation fails.
func (r *Client) GetTemplates(authTokenString string) (templates []Template, err error) {
	templates, err = r.Driver.GetTemplates(authTokenString)
	return
}

// GetTemplateById retrieves a template by its ID.
//
// Parameters:
// - id: The ID of the template to retrieve.
//
// Returns:
// - template: The retrieved template.
// - err: An error if the retrieval fails.
func (r *Client) GetTemplateById(id string, authString string) (template Template, err error) {
	template, err = r.Driver.GetTemplateById(id, authString)
	return
}

// CreateReport creates a report with the given ID and data.
//
// Parameters:
// - id: The ID of the report to create.
// - data: A map of report objects.
// - authTokenString: The authentication token string.
//
// Returns:
// - err: An error if the operation fails.
func (r *Client) CreateReport(report Report, authTokenString string) (resultReport Report, err error) {
	dbReport, err := r.GetReport(report.Id, authTokenString)
	if errors.Is(err, mongo.ErrNoDocuments) || dbReport.Id == "" {
		dbReport, _ = r.SaveReport(report, authTokenString)
		report = dbReport
	} else if err != nil {
		return
	}
	report.ReportFiles = dbReport.ReportFiles
	reportData, err := r.setReportData(report.Data, authTokenString)
	if err != nil {
		return
	}
	reportFileId, reportFileType, reportFileLink, err := r.Driver.CreateReport(report.Name, report.TemplateName, reportData, authTokenString)
	if err != nil {
		return
	}
	report.ReportFiles = append(report.ReportFiles, ReportFile{Id: reportFileId, Type: reportFileType, Link: reportFileLink})
	err = r.UpdateReport(report, authTokenString)
	if err != nil {
		return
	}
	resultReport = report
	return
}

// setReportData recursively sets report data based on the input data and authorization token.
//
// Parameters:
// - data: A map of ReportObject containing the report data.
// - authToken: A string representing the authorization token.
// Returns:
// - resultData: A map of interface{} containing the processed report data.
// - err: An error if the operation fails.
func (r *Client) setReportData(data map[string]ReportObject, authToken string) (resultData map[string]interface{}, err error) {
	resultData = make(map[string]interface{}, len(data))
	for key, value := range data {
		switch value.ValueType {
		case "string", "int", "float", "float64":
			if value.Value != nil {
				resultData[key] = value.Value
			} else if value.Query != nil {
				var responseData []interface{}
				responseData, err = r.DBClient.Query(authToken, *value.Query)
				if err != nil {
					return
				}
				if len(responseData) > 0 {
					resultData[key] = responseData[0]
				}
			}
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

// DownloadReportFile downloads a report file with the given file ID from the given report.
//
// Parameters:
// - reportId: The ID of the report to download the file from.
// - fileId: The ID of the file to download.
// - authTokenString: The authentication token string.
//
// Returns:
// - content: The content of the file.
// - contentType: The content type of the file.
// - err: An error if the operation fails.
func (r *Client) DownloadReportFile(reportId string, fileId string, authTokenString string) (content []byte, contentType string, err error) {
	_, err = r.GetReport(reportId, authTokenString)
	if err != nil {
		return
	}
	content, contentType, err = r.Driver.GetReportContent(fileId, authTokenString)
	if err != nil {
		return
	}
	return content, contentType, err
}

// DeleteCreatedReportFile deletes a report file with the given file ID from the given report.
//
// Parameters:
// - reportId: The ID of the report to delete the file from.
// - fileId: The ID of the file to delete.
// - authTokenString: The authentication token string.
//
// Returns:
// - err: An error if the operation fails.
func (r *Client) DeleteCreatedReportFile(reportId string, fileId string, authTokenString string) (err error) {
	report, err := r.GetReport(reportId, authTokenString)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	err = r.Driver.DeleteCreatedReportFile(fileId, authTokenString)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	for index, element := range report.ReportFiles {
		if element.Id == fileId {
			report.ReportFiles = append(report.ReportFiles[:index], report.ReportFiles[index+1:]...)
		}
	}
	err = r.UpdateReport(report, authTokenString)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	return
}

// SaveReport saves a report to the MongoDB database.
//
// Parameters:
// - templateId: A string representing the ID of the report template.
// - data: A map of ReportObject containing the report data.
// - authTokenString: A string representing the authorization token.
//
// Returns:
// - err: An error if the operation fails.
func (r *Client) SaveReport(report Report, authTokenString string) (savedReport Report, err error) {
	claims, err := jwt.Parse(authTokenString)
	if err != nil {
		return
	}
	report.Id = uuid.New().String()
	report.UserId = claims.GetUserId()
	_, err = Reports().InsertOne(CTX, report)
	savedReport = report
	return
}

// UpdateReport updates a report in the MongoDB database.
//
// Parameters:
// - report: A Report representing the report to update.
// - authTokenString: A string representing the authorization token.
//
// Returns:
// - err: An error if the operation fails.
func (r *Client) UpdateReport(report Report, authTokenString string) (err error) {
	claims, err := jwt.Parse(authTokenString)
	if err != nil {
		return
	}
	report.UserId = claims.GetUserId()
	if len(report.ReportFiles) == 0 || report.ReportFiles == nil {
		oldReport, e := r.GetReport(report.Id, authTokenString)
		if e != nil {
			return
		}
		report.ReportFiles = oldReport.ReportFiles
	}
	_, err = Reports().ReplaceOne(CTX, bson.M{"_id": report.Id, "userid": claims.GetUserId()}, report, options.Replace().SetUpsert(true))
	return
}

// DeleteReport deletes a report by its ID.
//
// Parameters:
// - id: The ID of the report to delete.
// - authTokenString: The authentication token string.
// - admin: A boolean indicating whether the deletion is performed by an admin.
//
// Returns:
// - err: An error if the operation fails.
func (r *Client) DeleteReport(id string, authTokenString string, admin bool) (err error) {
	claims, err := jwt.Parse(authTokenString)
	if err != nil {
		return
	}
	req := bson.M{"_id": id, "userid": claims.GetUserId()}
	if admin {
		req = bson.M{"_id": id}
	}
	res := Reports().FindOneAndDelete(CTX, req)
	return res.Err()
}

// GetReport retrieves a report from the MongoDB database based on the provided ID and authentication token.
//
// Parameters:
// - id: A string representing the ID of the report to retrieve.
// - authTokenString: A string representing the authentication token.
//
// Returns:
// - report: A Report struct representing the retrieved report.
// - err: An error if the operation fails.
func (r *Client) GetReport(id string, authTokenString string) (report Report, err error) {
	claims, err := jwt.Parse(authTokenString)
	if err != nil {
		return
	}
	err = Reports().FindOne(CTX, bson.M{"_id": id, "userid": claims.GetUserId()}).Decode(&report)
	if err != nil {
		return Report{}, err
	}
	return
}

// GetReports retrieves a list of reports from the MongoDB database based on the provided authentication token and query arguments.
//
// Parameters:
// - authTokenString: A string representing the authentication token.
// - args: A map of query arguments, including limit, offset, order, and search.
// - admin: A boolean indicating whether the retrieval is performed by an admin.
//
// Returns:
// - reports: A slice of Report structs representing the retrieved reports.
// - err: An error if the operation fails.
func (r *Client) GetReports(authTokenString string, args map[string][]string, admin bool) (reports []Report, err error) {
	claims, err := jwt.Parse(authTokenString)
	if err != nil {
		return
	}
	opt := options.Find()
	for arg, value := range args {
		if arg == "limit" {
			limit, _ := strconv.ParseInt(value[0], 10, 64)
			opt.SetLimit(limit)
		}
		if arg == "offset" {
			skip, _ := strconv.ParseInt(value[0], 10, 64)
			opt.SetSkip(skip)
		}
		if arg == "order" {
			ord := strings.Split(value[0], ":")
			order := 1
			if ord[1] == "desc" {
				order = -1
			}
			opt.SetSort(bson.M{ord[0]: int64(order)})
		}
	}
	var cur *mongo.Cursor
	req := bson.M{"userid": claims.GetUserId()}
	if val, ok := args["search"]; ok {
		req = bson.M{"userid": claims.GetUserId(), "_id": bson.RegEx{Pattern: val[0], Options: "i"}}
	}
	if admin {
		req = bson.M{}
	}
	cur, err = Reports().Find(CTX, req, opt)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	for cur.Next(CTX) {
		// create a value into which the single document can be decoded
		var elem Report
		err := cur.Decode(&elem)
		if err != nil {
			log.Fatal(err)
			return nil, err
		}
		reports = append(reports, elem)
	}
	return
}
