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
	sb_config_types "github.com/SENERGY-Platform/go-service-base/config-hdl/types"
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

// FiguresConfig points at the figures service. It is only reachable through the
// gateway, so the url includes the gateway's path for it. Empty disables figuresQuery.
type FiguresConfig struct {
	Url string `json:"url" env_var:"FIGURES_URL"`
	// Timeout bounds a call to figures and to the device repository for a figures query.
	Timeout string `json:"timeout" env_var:"FIGURES_TIMEOUT"`
}

// TemplateConfig enables writing templates. Both values have to be set, otherwise
// the endpoints that create, change, delete and preview templates answer 404.
type TemplateConfig struct {
	// Folder is the name of the jsreport folder templates are created in.
	Folder string `json:"folder" env_var:"TEMPLATE_FOLDER"`
	// AuthorRole is the realm role a caller needs to write templates.
	AuthorRole string `json:"author_role" env_var:"TEMPLATE_AUTHOR_ROLE"`
	// GroupClaim is the token claim whose value names the caller's subfolder of Folder.
	// It is the claim jsreport reads the caller's group from.
	GroupClaim string `json:"group_claim" env_var:"TEMPLATE_GROUP_CLAIM"`
}

// Enabled reports whether templates may be written at all.
func (c TemplateConfig) Enabled() bool {
	return c.Folder != "" && c.AuthorRole != ""
}

type KeycloakConfig struct {
	Url          string                 `json:"url" env_var:"KEYCLOAK_URL"`
	ClientId     string                 `json:"client_id" env_var:"KEYCLOAK_CLIENT_ID"`
	ClientSecret sb_config_types.Secret `json:"client_secret" env_var:"KEYCLOAK_CLIENT_SECRET"`
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
	Figures                 FiguresConfig  `json:"figures"`
	Templates               TemplateConfig `json:"templates"`
	Keycloak                KeycloakConfig `json:"keycloak"`
	Mail                    MailConfig     `json:"mail"`
	SchedulerTickerDuration string         `json:"scheduler_ticker_duration" env_var:"SCHEDULER_TICKER_DURATION"`
	// MongoUrl is passed to the driver unchanged and logged with the config, so
	// credentials belong in MongoUser and MongoPassword.
	MongoUrl        string                 `json:"mongo_url" env_var:"MONGO_URL"`
	MongoUser       string                 `json:"mongo_user" env_var:"MONGO_USER"`
	MongoPassword   sb_config_types.Secret `json:"mongo_password" env_var:"MONGO_PASSWORD"`
	MongoAuthSource string                 `json:"mongo_auth_source" env_var:"MONGO_AUTH_SOURCE"`
	MongoDatabase   string                 `json:"mongo_database" env_var:"MONGO_DATABASE"`
	// ReportJobWorkers is how many report files may be built at the same time. It
	// bounds the load this service puts on jsreport and the timescale wrapper.
	ReportJobWorkers int `json:"report_job_workers" env_var:"REPORT_JOB_WORKERS"`
	// ReportJobRetention is how long a finished report job stays queryable.
	ReportJobRetention string `json:"report_job_retention" env_var:"REPORT_JOB_RETENTION"`
	// ReportJobStaleAfter is how long a running job may go without a heartbeat
	// before it is treated as interrupted. Has to exceed the heartbeat interval.
	ReportJobStaleAfter string `json:"report_job_stale_after" env_var:"REPORT_JOB_STALE_AFTER"`
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
		Figures: FiguresConfig{
			Timeout: "2m",
		},
		Templates: TemplateConfig{
			GroupClaim: "group",
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
		MongoAuthSource:         "admin",
		MongoDatabase:           "reporting",
		ReportJobWorkers:        2,
		ReportJobRetention:      "168h",
		ReportJobStaleAfter:     "2m",
	}
	err := sb_config_hdl.Load(&cfg, nil, envTypeParser, nil, path)
	return &cfg, err
}
