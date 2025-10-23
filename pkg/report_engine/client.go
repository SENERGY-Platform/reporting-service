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
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/SENERGY-Platform/reporting-service/pkg/apis/senergy_devices"
	"github.com/SENERGY-Platform/reporting-service/pkg/config"
	"github.com/SENERGY-Platform/reporting-service/pkg/models"

	"github.com/SENERGY-Platform/reporting-service/pkg/apis/senergy_db_v3"
	"github.com/SENERGY-Platform/service-commons/pkg/jwt"
	"github.com/globalsign/mgo/bson"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var dataPointsTSDBCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "reporting_queried_datapoints_tsdb_total",
	Help: "Total number of data points queried from Timescale DB for report creation",
}, []string{"user_id", "report_id"})

type Client struct {
	Driver        ReportingDriver
	DBClient      *senergy_db_v3.Client
	DevicesClient *senergy_devices.Client
	Config        *config.Config
}

// NewClient creates a new client with the given reporting driver.
//
// Parameters:
// - driver: The reporting driver to use.
//
// Returns:
// - client: The newly created client.
func NewClient(driver ReportingDriver, config *config.Config) *Client {
	dbClient := senergy_db_v3.NewClient(
		config.SNRGY.Url,
		config.SNRGY.Port,
	)
	devicesClient := senergy_devices.NewClient(
		config.SNRGY.Url,
		config.SNRGY.Port,
	)
	return &Client{Driver: driver, DBClient: dbClient, DevicesClient: devicesClient, Config: config}
}

// GetTemplates retrieves a list of available report templates.
//
// Returns a slice of Template objects and an error if the operation fails.
func (r *Client) GetTemplates(authTokenString string) (templates []models.Template, err error) {
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
func (r *Client) GetTemplateById(id string, authString string) (template models.Template, err error) {
	template, err = r.Driver.GetTemplateById(id, authString)
	return
}

func (r *Client) GetTemplatePreviewById(id string, authString string) (content []byte, contentType string, fileTypeExtension string, err error) {
	content, contentType, fileTypeExtension, err = r.Driver.GetTemplatePreview(id, authString)
	return
}

// CreateReportFile creates a report file with the given ID and data.
//
// Parameters:
// - id: The ID of the report to create.
// - data: A map of report objects.
// - authTokenString: The authentication token string.
//
// Returns:
// - err: An error if the operation fails.
func (r *Client) CreateReportFile(reportRequest models.Report, authTokenString string) (resultReport models.Report, reportFileId string, err error) {
	reportModel, err := r.GetReportModel(reportRequest.Id, authTokenString)
	// if no report model is found, create a new one
	if errors.Is(err, mongo.ErrNoDocuments) || reportModel.Id == "" {
		reportModel, _ = r.SaveReportModel(reportRequest, authTokenString)
		reportRequest = reportModel
	} else if err != nil {
		return
	}
	reportRequest.ReportFiles = reportModel.ReportFiles

	// set report file data
	reportData, err := r.setReportFileData(reportRequest.Data, authTokenString, reportRequest.Id)
	if err != nil {
		return
	}

	// create the actual report file using the underlying driver
	reportFileId, reportFileType, reportFileLink, err := r.Driver.CreateReport(reportRequest.Name, reportRequest.TemplateName, reportData, authTokenString)
	if err != nil {
		return
	}

	// add the report file model to the report model
	reportRequest.ReportFiles = append(reportRequest.ReportFiles, models.ReportFile{Id: reportFileId, Type: reportFileType, Link: reportFileLink, CreatedAt: time.Now()})
	reportRequest.CreatedAt = reportModel.CreatedAt
	err = r.UpdateReportModel(reportRequest, authTokenString)
	if err != nil {
		return
	}

	resultReport = reportRequest
	return
}

// setReportFileData recursively sets report data based on the input data and authorization token.
//
// Parameters:
// - data: A map of ReportObject containing the report data.
// - authToken: A string representing the authorization token.
// Returns:
// - resultData: A map of interface{} containing the processed report data.
// - err: An error if the operation fails.
func (r *Client) setReportFileData(data map[string]models.ReportObject, authToken string, reportId string) (resultData map[string]interface{}, err error) {
	resultData = make(map[string]interface{}, len(data))
	claims, err := jwt.Parse(authToken)
	if err != nil {
		return
	}
	userId := claims.GetUserId()
	for key, value := range data {
		switch value.ValueType {
		case "string", "int", "float", "float64":
			if value.Value != nil {
				resultData[key] = value.Value
			} else if value.Query != nil {
				var responseData []interface{}
				err = r.updateStartAndEndDate(&value)
				if err != nil {
					return
				}
				responseData, err = r.DBClient.Query(authToken, *value.Query, *value.QueryOptions)
				if err != nil {
					return
				}
				dataPointsTSDBCounter.WithLabelValues(userId, reportId).Add(float64(len(responseData)))
				responseData = r.filterQueryValues(responseData)
				if len(responseData) > 0 {
					resultData[key] = responseData[0]
				}
			} else {
				delete(resultData, key)
			}
		case "object":
			resultData[key], err = r.setReportFileData(value.Fields, authToken, reportId)
			if err != nil {
				return
			}
			if len(resultData[key].(map[string]interface{})) == 0 {
				delete(resultData, key)
			}
		case "array":
			if value.Value != nil {
				resultData[key] = value.Value
			} else if len(value.Children) > 0 {
				var arrayData map[string]interface{}
				arrayData, err = r.setReportFileData(value.Children, authToken, reportId)
				if err != nil {
					return
				}
				// convert map[string]interface{} to []interface{}
				var dataSlice []interface{}
				//order slice
				var keys []int
				for k := range arrayData {
					var i int
					i, err = strconv.Atoi(k)
					if err != nil {
						return
					}
					keys = append(keys, i)
				}
				sort.Ints(keys)
				for _, k := range keys {
					dataSlice = append(dataSlice, arrayData[strconv.Itoa(k)])
				}
				resultData[key] = dataSlice
				if len(resultData[key].([]interface{})) == 0 {
					delete(resultData, key)
				}
			} else if value.Query != nil {
				var responseData []interface{}
				err = r.updateStartAndEndDate(&value)
				if err != nil {
					return nil, err
				}
				responseData, err = r.DBClient.Query(authToken, *value.Query, *value.QueryOptions)
				if err != nil {
					return
				}
				dataPointsTSDBCounter.WithLabelValues(userId, reportId).Add(float64(len(responseData)))
				responseData = r.filterQueryValues(responseData)
				resultData[key] = responseData
			} else if value.DeviceQuery != nil {
				var responseData []interface{}
				responseData, err = r.DevicesClient.Query(authToken, *value.DeviceQuery.Last)
				resultData[key] = responseData
			}
		}
	}
	return
}

func (r *Client) filterQueryValues(queryValues []interface{}) (filteredData []interface{}) {
	for _, value := range queryValues {
		if value != nil {
			filteredData = append(filteredData, value)
		} else {
			filteredData = append(filteredData, 0)
		}
	}
	return
}

func (r *Client) updateStartAndEndDate(object *models.ReportObject) (err error) {
	if object.QueryOptions != nil {
		if object.QueryOptions.RollingStartDate != nil && object.Query.Time.Start != nil {
			startDate, e := time.Parse(time.RFC3339, *object.Query.Time.Start)
			if e != nil {
				return
			}
			startDate = startDate.Add(time.Minute * time.Duration(-*object.QueryOptions.StartOffset))
			newDate := startDate
			switch *object.QueryOptions.RollingStartDate {
			case "month":
				newDate = time.Date(time.Now().Year(), time.Now().Month(), startDate.Day(), 0, 0, 0, 0, time.UTC)
			case "year":
				newDate = time.Date(time.Now().Year(), startDate.Month(), startDate.Day(), 0, 0, 0, 0, time.UTC)
			}
			newDate = newDate.Add(time.Minute * time.Duration(*object.QueryOptions.StartOffset))
			*object.Query.Time.Start = newDate.Format(time.RFC3339)
		}
		if object.QueryOptions.RollingEndDate != nil && object.Query.Time.End != nil {
			endDate, e := time.Parse(time.RFC3339, *object.Query.Time.End)
			if e != nil {
				return
			}
			endDate = endDate.Add(time.Minute * time.Duration(-*object.QueryOptions.EndOffset))
			newDate := endDate
			switch *object.QueryOptions.RollingEndDate {
			case "month":
				newDate = time.Date(time.Now().Year(), time.Now().Month(), endDate.Day(), 0, 0, 0, 0, time.UTC)
			case "year":
				newDate = time.Date(time.Now().Year(), endDate.Month(), endDate.Day(), 0, 0, 0, 0, time.UTC)
			}
			newDate = newDate.Add(time.Minute * time.Duration(*object.QueryOptions.EndOffset))
			*object.Query.Time.End = newDate.Format(time.RFC3339)
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
// - fileTypeExtension: The file type extension of the report.
// - err: An error if the operation fails.
func (r *Client) DownloadReportFile(reportId string, fileId string, authTokenString string) (content []byte, contentType string, fileTypeExtension string, err error) {
	_, err = r.GetReportModel(reportId, authTokenString)
	if err != nil {
		return
	}
	content, contentType, fileTypeExtension, err = r.Driver.GetReportContent(fileId, authTokenString)
	if err != nil {
		return
	}
	return content, contentType, fileTypeExtension, err
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
	report, err := r.GetReportModel(reportId, authTokenString)
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
	err = r.UpdateReportModel(report, authTokenString)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	return
}

// SaveReportModel saves a report to the MongoDB database.
//
// Parameters:
// - templateId: A string representing the ID of the report template.
// - data: A map of ReportObject containing the report data.
// - authTokenString: A string representing the authorization token.
//
// Returns:
// - err: An error if the operation fails.
func (r *Client) SaveReportModel(report models.Report, authTokenString string) (savedReport models.Report, err error) {
	claims, err := jwt.Parse(authTokenString)
	if err != nil {
		return
	}
	report.Id = uuid.New().String()
	report.UserId = claims.GetUserId()
	ts, err := calculateNextSchedule(report)
	if err != nil {
		return
	}
	report.ScheduledFor = ts
	report.CreatedAt = time.Now()
	_, err = Reports().InsertOne(CTX, report)
	savedReport = report
	return
}

// UpdateReportModel updates a report in the MongoDB database.
//
// Parameters:
// - report: A Report representing the report to update.
// - authTokenString: A string representing the authorization token.
//
// Returns:
// - err: An error if the operation fails.
func (r *Client) UpdateReportModel(report models.Report, authTokenString string) (err error) {
	claims, err := jwt.Parse(authTokenString)
	if err != nil {
		return
	}
	report.UserId = claims.GetUserId()
	ts, err := calculateNextSchedule(report)
	if err != nil {
		return
	}
	report.ScheduledFor = ts
	report.UpdatedAt = time.Now()
	if report.ReportFiles == nil {
		oldReport, e := r.GetReportModel(report.Id, authTokenString)
		if e != nil {
			return
		}
		report.ReportFiles = oldReport.ReportFiles
		report.CreatedAt = oldReport.CreatedAt
	}
	_, err = Reports().ReplaceOne(CTX, bson.M{"_id": report.Id, "userid": claims.GetUserId()}, report, options.Replace().SetUpsert(true))
	return
}

// DeleteReport deletes a report model and created files by its ID.
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
	report, err := r.GetReportModel(id, authTokenString)
	if err != nil {
		return
	}
	for _, element := range report.ReportFiles {
		err = r.Driver.DeleteCreatedReportFile(element.Id, authTokenString)
		if err != nil {
			return
		}
	}
	res := Reports().FindOneAndDelete(CTX, req)
	return res.Err()
}

// GetReportModel GetReport retrieves a report from the MongoDB database based on the provided ID and authentication token.
//
// Parameters:
// - id: A string representing the ID of the report to retrieve.
// - authTokenString: A string representing the authentication token.
//
// Returns:
// - report: A Report struct representing the retrieved report.
// - err: An error if the operation fails.
func (r *Client) GetReportModel(id string, authTokenString string) (report models.Report, err error) {
	claims, err := jwt.Parse(authTokenString)
	if err != nil {
		return
	}
	err = Reports().FindOne(CTX, bson.M{"_id": id, "userid": claims.GetUserId()}).Decode(&report)
	if err != nil {
		return models.Report{}, err
	}
	return
}

// GetReportModels retrieves a list of reports from the MongoDB database based on the provided authentication token and query arguments.
//
// Parameters:
// - authTokenString: A string representing the authentication token.
// - args: A map of query arguments, including limit, offset, order, and search.
// - admin: A boolean indicating whether the retrieval is performed by an admin.
//
// Returns:
// - reports: A slice of Report structs representing the retrieved reports.
// - err: An error if the operation fails.
func (r *Client) GetReportModels(authTokenString string, args map[string][]string, admin bool) (reports []models.Report, err error) {
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
		var elem models.Report
		err := cur.Decode(&elem)
		if err != nil {
			log.Fatal(err)
			return nil, err
		}
		reports = append(reports, elem)
	}
	return
}

// RunScheduler regularly checks if any reports need to be created based on their cron schedule and handles report creation accordingly.
// The method blocks until any error occurs.
//
// Parameters:
//
// Returns:
// - err: An error if the operation fails.
func (r *Client) RunScheduler() error {
	tickerDur, err := time.ParseDuration(r.Config.SchedulerTickerDuration)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(tickerDur)
	for {
		<-ticker.C
		cur, err := Reports().Find(CTX, bson.M{"scheduledfor": bson.M{"$lt": time.Now()}})
		if err != nil {
			return err
		}
		for cur.Next(CTX) {
			var report models.Report
			err := cur.Decode(&report)
			if err != nil {
				return err
			}
			log.Println("Creating scheduled report file for " + report.Id)
			token, _, err := jwt.ExchangeUserToken(
				r.Config.Keycloak.Url,
				r.Config.Keycloak.ClientId,
				r.Config.Keycloak.ClientSecret,
				report.UserId,
			)
			if err != nil {
				return err
			}
			_, reportFileId, err := r.CreateReportFile(report, token.Token) // already calculates and saves next schedule
			if err != nil {
				return err
			}
			_, err = r.EmailReport(reportFileId, report, token.Token)
			if err != nil {
				return err
			}
		}
	}
}

// EmailReport sends the specified report file to the email adrdesses specified in the report
//
// Parameters:
// - reportFileId: File ID of the file to send
// - report: Report
// - token: Token of the user to send the report file to
//
// Returns:
// - sent: true if an email has been sent, false otherwise
// - err: An error if the operation fails.
func (r *Client) EmailReport(reportFileId string, report models.Report, token string) (sent bool, err error) {
	if len(report.EmailReceivers) == 0 {
		return false, nil
	}
	b, contentType, fileTypeExtension, err := r.DownloadReportFile(report.Id, reportFileId, token)
	if err != nil {
		return false, err
	}
	subject := report.EmailSubject
	if len(subject) == 0 {
		subject = r.Config.Mail.Subject
	}
	text := report.EmailText
	if len(text) == 0 {
		text = r.Config.Mail.Text
	}
	email := models.SendRequest{
		Bcc: report.EmailReceivers,
		From: models.FromTo{
			Email: r.Config.Mail.From,
		},
		Attachments: []struct {
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
		}{{
			Content:     base64.StdEncoding.EncodeToString(b),
			ContentType: contentType,
			Filename:    reportFileId + "." + fileTypeExtension,
		}},
		Subject: subject,
		Text:    text,
		HTML:    report.EmailHTML,
	}
	_, err = email.Send(r.Config.Mail.MailpitUrl)
	if err != nil {
		return false, err
	}
	return true, nil
}

func calculateNextSchedule(r models.Report) (t *time.Time, err error) {
	if len(r.Cron) == 0 {
		return nil, nil
	}
	schedule, err := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(r.Cron)
	if err != nil {
		return nil, err
	}
	ts := schedule.Next(time.Now())
	return &ts, err
}
