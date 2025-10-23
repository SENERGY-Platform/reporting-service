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

package config

import (
	sb_config_hdl "github.com/SENERGY-Platform/go-service-base/config-hdl"
)

type LoggerConfig struct {
	Level string `json:"level" env_var:"LOGGER_LEVEL"`
}

type JSReportConfig struct {
	Url  string `json:"url" env_var:"JSREPORT_SERVER_URL"`
	Port int64  `json:"port" env_var:"JSREPORT_SERVER_PORT"`
}

type SNRGYConfig struct {
	Url  string `json:"url" env_var:"SENERGY_DB_URL"`
	Port int64  `json:"port" env_var:"SENERGY_DB_PORT"`
}

type KeycloakConfig struct {
	Url          string `json:"url" env_var:"KEYCLOAK_URL"`
	ClientId     string `json:"client_id" env_var:"KEYCLOAK_CLIENT_ID"`
	ClientSecret string `json:"client_secret" env_var:"KEYCLOAK_CLIENT_SECRET"`
}

type MailConfig struct {
	MailpitUrl string `json:"mailpit_url" env_var:"MAILPIT_URL"`
	From       string `json:"from" env_var:"EMAIL_FROM"`
	Subject    string `json:"subject" env_var:"EMAIL_SUBJECT"`
	Text       string `json:"text" env_var:"EMAIL_TEXT"`
}

type Config struct {
	Logger                  LoggerConfig   `json:"logger" env_var:"LOGGER_CONFIG"`
	URLPrefix               string         `json:"url_prefix" env_var:"URL_PREFIX"`
	ServerPort              int            `json:"server_port" env_var:"SERVER_PORT"`
	Debug                   bool           `json:"debug" env_var:"DEBUG"`
	JSReport                JSReportConfig `json:"jsreport"`
	SNRGY                   SNRGYConfig    `json:"snrgy"`
	Keycloak                KeycloakConfig `json:"keycloak"`
	Mail                    MailConfig     `json:"mail"`
	SchedulerTickerDuration string         `json:"scheduler_ticker_duration" env_var:"SCHEDULER_TICKER_DURATION"`
	MongoUrl                string         `json:"mongo_url" env_var:"MONGODB_URI"`
}

func New(path string) (*Config, error) {
	cfg := Config{
		ServerPort: 8080,
		Debug:      false,
		JSReport: JSReportConfig{
			Url:  "http://localhost",
			Port: 5488,
		},
		SNRGY: SNRGYConfig{
			Url:  "http://localhost",
			Port: 80,
		},
		Keycloak: KeycloakConfig{
			Url:          "http://localhost",
			ClientId:     "reporting-service",
			ClientSecret: "reporting-service",
		},
		Mail: MailConfig{
			MailpitUrl: "http://mailpit.notifier:8025",
			From:       "reporting-service@localhost",
			Subject:    "Report",
			Text:       "Report attached to this email",
		},
		SchedulerTickerDuration: "1m",
		MongoUrl:                "mongodb://localhost:27017",
	}
	err := sb_config_hdl.Load(&cfg, nil, envTypeParser, nil, path)
	return &cfg, err
}
