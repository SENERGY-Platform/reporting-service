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

type Template struct {
	Name string `json:"name,omitempty"`
	Id   string `json:"id,omitempty"`
	Data Data   `json:"data,omitempty"`
}

type Data struct {
	Name           string              `json:"name,omitempty"`
	Id             string              `json:"id,omitempty"`
	DataJSONString string              `json:"dataJsonString,omitempty"`
	DataStructured map[string]DataType `json:"dataStructured,omitempty"`
}
type DataType struct {
	Name      string              `json:"name,omitempty"`
	ValueType string              `json:"valueType,omitempty"`
	Length    int                 `json:"length,omitempty"`
	Fields    map[string]DataType `json:"fields,omitempty"`
	Children  map[string]DataType `json:"children,omitempty"`
}