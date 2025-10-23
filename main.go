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

package main

import (
	"fmt"
	"os"

	"github.com/SENERGY-Platform/go-service-base/srv-info-hdl"
	sb_util "github.com/SENERGY-Platform/go-service-base/util"
	"github.com/SENERGY-Platform/reporting-service/pkg/apis/jsreport"
	"github.com/SENERGY-Platform/reporting-service/pkg/config"
	"github.com/SENERGY-Platform/reporting-service/pkg/report_engine"
	"github.com/SENERGY-Platform/reporting-service/pkg/server"
	"github.com/SENERGY-Platform/reporting-service/pkg/util"
)

var Version = "0.0.42"

func main() {
	ec := 0
	defer func() {
		os.Exit(ec)
	}()

	srvInfoHdl := srv_info_hdl.New("reporting-service", Version)

	config.ParseFlags()

	cfg, err := config.New(config.ConfPath)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		ec = 1
		return
	}

	util.InitStructLogger(cfg.Logger.Level)

	util.Logger.Info(srvInfoHdl.Name(), "version", srvInfoHdl.Version())
	util.Logger.Info("config: " + sb_util.ToJsonStr(cfg))

	client := report_engine.NewClient(jsreport.NewJSReportClient(cfg.JSReport.Url, cfg.JSReport.Port), cfg)
	go func() {
		err = client.RunScheduler()
		if err != nil {
			util.Logger.Error("could not start scheduler", "error", err)
			ec = 1
			return
		}
	}()
	report_engine.InitDB(cfg.MongoUrl)
	defer report_engine.CloseDB()
	server.StartAPI(client, *cfg)
}
