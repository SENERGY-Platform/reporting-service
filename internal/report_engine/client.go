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
	"github.com/SENERGY-Platform/service-commons/pkg/jwt"
	"github.com/globalsign/mgo/bson"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"log"
	"report-service/internal/apis/senergy_db_v3"
	"report-service/internal/helper"
	"strconv"
	"strings"
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

// GetTemplates retrieves a list of available report templates.
//
// Returns a slice of Template objects and an error if the operation fails.
func (r *Client) GetTemplates() (templates []Template, err error) {
	templates, err = r.Driver.GetTemplates()
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
func (r *Client) GetTemplateById(id string) (template Template, err error) {
	template, err = r.Driver.GetTemplateById(id)
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
func (r *Client) CreateReport(report Report, authTokenString string) (err error) {
	reportData, err := r.setReportData(report.Data, authTokenString)
	err = r.Driver.CreateReport(report.TemplateName, reportData)
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

// SaveReport saves a report to the MongoDB database.
//
// Parameters:
// - templateId: A string representing the ID of the report template.
// - data: A map of ReportObject containing the report data.
// - authTokenString: A string representing the authorization token.
//
// Returns:
// - err: An error if the operation fails.
func (r *Client) SaveReport(report Report, authTokenString string) (err error) {
	claims, err := jwt.Parse(authTokenString)
	report.Id = uuid.New().String()
	report.UserId = claims.GetUserId()
	_, err = Mongo().InsertOne(CTX, report)
	return
}

func (r *Client) UpdateReport(report Report, authTokenString string) (err error) {
	claims, err := jwt.Parse(authTokenString)
	report.UserId = claims.GetUserId()
	_, err = Mongo().ReplaceOne(CTX, bson.M{"_id": report.Id, "userid": claims.GetUserId()}, report)
	return
}

func (r *Client) DeleteReport(id string, authTokenString string, admin bool) (err error) {
	claims, err := jwt.Parse(authTokenString)
	req := bson.M{"_id": id, "userid": claims.GetUserId()}
	if admin {
		req = bson.M{"_id": id}
	}
	res := Mongo().FindOneAndDelete(CTX, req)
	return res.Err()
}

func (r *Client) GetReport(id string, authTokenString string) (report Report, err error) {
	claims, err := jwt.Parse(authTokenString)
	err = Mongo().FindOne(CTX, bson.M{"_id": id, "userid": claims.GetUserId()}).Decode(&report)
	if err != nil {
		log.Println(err)
		return Report{}, err
	}
	return
}

func (r *Client) GetReports(authTokenString string, args map[string][]string, admin bool) (reports []Report, err error) {
	claims, err := jwt.Parse(authTokenString)
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
	cur, err = Mongo().Find(CTX, req, opt)
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
