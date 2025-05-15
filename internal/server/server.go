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

package server

import (
	"log"
	"net/http"

	"github.com/SENERGY-Platform/report-service/internal/apis/jsreport"
	"github.com/SENERGY-Platform/report-service/internal/helper"
	"github.com/SENERGY-Platform/report-service/internal/report_engine"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func Start() {
	client := report_engine.NewClient(jsreport.NewJSReportClient(
		helper.GetEnv("JSREPORT_SERVER_URL", "http://localhost"),
		helper.GetEnv("JSREPORT_SERVER_PORT", "5488")))
	go func() {
		err := client.RunScheduler()
		if err != nil {
			log.Fatal("ERROR: " + err.Error())
		}
	}()
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		log.Println("INFO: Starting prometheus metrics on :2112/metrics")
		log.Println("WARNING: Metrics server exited: " + http.ListenAndServe(":2112", nil).Error())
	}()
	report_engine.InitDB()
	defer report_engine.CloseDB()
	startAPI(client)
}
