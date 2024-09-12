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
	timescaleModels "github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/go-resty/resty/v2"
	"net/http"
)

type Client struct {
	Url        string
	Port       string
	BaseUrl    string
	HttpClient *resty.Client
}

func NewClient(url string, port string) *Client {
	client := resty.New()
	return &Client{Url: url, Port: port, BaseUrl: fmt.Sprintf("%v:%v", url, port), HttpClient: client}
}

func (s *Client) Query(authTokenString string, query timescaleModels.QueriesRequestElement) (data []interface{}, err error) {
	if !query.Valid() {
		return data, errors.New("request not valid")
	}
	response, err := s.HttpClient.R().
		SetHeader("Authorization", authTokenString).
		SetBody([]timescaleModels.QueriesRequestElement{query}).
		Post(s.BaseUrl + "/db/v3/queries")
	if err != nil {
		return
	}
	if response.StatusCode() != http.StatusOK {
		err = fmt.Errorf("%v", response.String())
		return
	}
	var resp [][][]interface{}
	err = json.Unmarshal(response.Body(), &resp)
	if err != nil {
		return
	}
	for _, value := range resp[0] {
		data = append(data, value[1])
	}
	return data, err
}
