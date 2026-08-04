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

func (s *Client) Query(authTokenString string, query timescaleModels.QueriesRequestElement, queryOptions lib.QueryOptions) (data []interface{}, err error) {
	if !query.Valid() {
		return data, errors.New("request not valid")
	}
	response, err := s.HttpClient.R().
		SetHeader("Authorization", authTokenString).
		SetBody([]timescaleModels.QueriesRequestElement{query}).
		Post(s.BaseUrl + "/db/v3/queries/v2")
	if err != nil {
		return
	}
	if response.StatusCode() != http.StatusOK {
		return data, errors.New("senergy_db_v3.client - response code error: " + response.String())
	}
	var resp []timescaleModels.QueriesV2ResponseElement
	err = json.Unmarshal(response.Body(), &resp)
	if err != nil {
		return data, errors.New("senergy_db_v3.client - response unmarshal error: " + err.Error())
	}
	// A query that matched nothing comes back without any element or without any
	// series, so neither index may be taken for granted.
	if len(resp) == 0 || len(resp[0].Data) == 0 {
		return data, nil
	}
	resultObject := ""
	if queryOptions.ResultObject != nil {
		resultObject = *queryOptions.ResultObject
	}
	for _, value := range resp[0].Data[0] {
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
