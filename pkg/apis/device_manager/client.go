/*
 * Copyright 2025 InfAI (CC SES)
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

package device_manager

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	snrgyModels "github.com/SENERGY-Platform/models/go/models"
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

func (s *Client) Query(authTokenString string) (data []snrgyModels.Device, err error) {
	response, err := s.HttpClient.R().
		SetHeader("Authorization", authTokenString).
		Get(s.BaseUrl + "/device-manager/devices")
	if err != nil {
		return
	}
	if response.StatusCode() != http.StatusOK {
		return data, errors.New("senergy_devices.client - response code error: " + response.String())
	}
	err = json.Unmarshal(response.Body(), &data)
	return
}

// idsPerRequest keeps the ids filter well below common URL length limits.
const idsPerRequest = 50

// QueryByIds returns the given devices the token may read. The ids filter makes the
// service ignore its default page size of 100.
func (s *Client) QueryByIds(authTokenString string, ids []string) (data []snrgyModels.Device, err error) {
	for start := 0; start < len(ids); start += idsPerRequest {
		end := min(start+idsPerRequest, len(ids))
		response, err := s.HttpClient.R().
			SetHeader("Authorization", authTokenString).
			SetQueryParam("ids", strings.Join(ids[start:end], ",")).
			Get(s.BaseUrl + "/device-manager/devices")
		if err != nil {
			return nil, err
		}
		if response.StatusCode() != http.StatusOK {
			return nil, errors.New("device_manager.client - response code error: " + response.String())
		}
		var page []snrgyModels.Device
		if err = json.Unmarshal(response.Body(), &page); err != nil {
			return nil, err
		}
		data = append(data, page...)
	}
	return data, nil
}
