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

package senergy_db_v3

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/SENERGY-Platform/reporting-service/lib"
	timescaleModels "github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/go-resty/resty/v2"
)

type Client struct {
	Url        string
	Port       int64
	BaseUrl    string
	HttpClient *resty.Client
}

func NewClient(url string, port int64) *Client {
	client := resty.New()
	return &Client{Url: url, Port: port, BaseUrl: fmt.Sprintf("%v:%v", url, port), HttpClient: client}
}

// Query returns the values of a query and the number of rows fetched for them. A
// device group query is aggregated into one series.
func (s *Client) Query(authTokenString string, query timescaleModels.QueriesRequestElement, queryOptions lib.QueryOptions) (data []interface{}, fetched int, err error) {
	var aggregation string
	if query.DeviceGroupId != nil {
		mode, err := queryOptions.GroupMode()
		if err != nil {
			return nil, 0, errors.New("senergy_db_v3.client - " + err.Error())
		}
		if mode != lib.DeviceGroupModeAggregate {
			return nil, 0, errors.New("senergy_db_v3.client - device group mode " + mode + " does not yield a single series")
		}
		aggregation, err = queryOptions.GroupAggregation()
		if err != nil {
			return nil, 0, errors.New("senergy_db_v3.client - " + err.Error())
		}
		if err = validateAggregate(query, aggregation); err != nil {
			return nil, 0, err
		}
	}
	resp, err := s.queryV2(authTokenString, query)
	if err != nil {
		return data, 0, err
	}
	if query.DeviceGroupId != nil {
		rows, err := AggregateDeviceGroup(resp, query, aggregation)
		if err != nil {
			return nil, 0, err
		}
		data, err = mapRows(rows, queryOptions)
		return data, countRows(resp), err
	}
	// A query that matched nothing comes back without any element or without any
	// series, so neither index may be taken for granted.
	if len(resp) == 0 || len(resp[0].Data) == 0 {
		return data, 0, nil
	}
	data, err = mapRows(resp[0].Data[0], queryOptions)
	return data, len(data), err
}

// QueryPerDevice runs a device group query and returns the first series of every
// response element separately, without resolving device names, plus the number of
// rows fetched. Each series keeps the order and limit the wrapper applied to it.
func (s *Client) QueryPerDevice(authTokenString string, query timescaleModels.QueriesRequestElement, queryOptions lib.QueryOptions) (series []lib.DeviceSeries, fetched int, err error) {
	if query.DeviceGroupId == nil {
		return nil, 0, errors.New("senergy_db_v3.client - per device results need a device group query")
	}
	resp, err := s.queryV2(authTokenString, query)
	if err != nil {
		return nil, 0, err
	}
	series = make([]lib.DeviceSeries, 0, len(resp))
	for _, element := range resp {
		if element.DeviceId == nil {
			return nil, 0, errors.New("senergy_db_v3.client - device group response element without device id")
		}
		entry := lib.DeviceSeries{DeviceId: *element.DeviceId}
		if element.ServiceId != nil {
			entry.ServiceId = *element.ServiceId
		}
		if len(element.Data) > 0 {
			entry.Values, err = mapRows(element.Data[0], queryOptions)
			if err != nil {
				return nil, 0, fmt.Errorf("%w (%s)", err, deviceLabel(element))
			}
		}
		series = append(series, entry)
	}
	return series, countRows(resp), nil
}

// countRows counts every row of every series, including series a result does not use.
func countRows(resp []timescaleModels.QueriesV2ResponseElement) (n int) {
	for _, element := range resp {
		for _, rows := range element.Data {
			n += len(rows)
		}
	}
	return n
}

func (s *Client) queryV2(authTokenString string, query timescaleModels.QueriesRequestElement) (resp []timescaleModels.QueriesV2ResponseElement, err error) {
	if !query.Valid() {
		return nil, errors.New("request not valid")
	}
	response, err := s.HttpClient.R().
		SetHeader("Authorization", authTokenString).
		SetBody([]timescaleModels.QueriesRequestElement{query}).
		Post(s.BaseUrl + "/db/v3/queries/v2")
	if err != nil {
		return
	}
	if response.StatusCode() != http.StatusOK {
		return nil, errors.New("senergy_db_v3.client - response code error: " + response.String())
	}
	err = json.Unmarshal(response.Body(), &resp)
	if err != nil {
		return nil, errors.New("senergy_db_v3.client - response unmarshal error: " + err.Error())
	}
	return resp, nil
}

// mapRows turns the rows of one series into report values as selected by resultObject.
func mapRows(rows [][]interface{}, queryOptions lib.QueryOptions) (data []interface{}, err error) {
	resultObject := ""
	if queryOptions.ResultObject != nil {
		resultObject = *queryOptions.ResultObject
	}
	for _, value := range rows {
		switch resultObject {
		case "key":
			if queryOptions.ResultKey == nil {
				return nil, errors.New("senergy_db_v3.client - result object 'key' needs a result key")
			}
			if *queryOptions.ResultKey < 0 || *queryOptions.ResultKey >= len(value) {
				return nil, errors.New("senergy_db_v3.client - result key out of range: " + strconv.Itoa(*queryOptions.ResultKey))
			}
			data = append(data, value[*queryOptions.ResultKey])
		case "array":
			data = append(data, value)
		default:
			// column 0 is the time stamp, column 1 the value that was selected
			if len(value) < 2 {
				return nil, errors.New("senergy_db_v3.client - response row has no value column")
			}
			data = append(data, value[1])
		}
	}
	return data, nil
}
