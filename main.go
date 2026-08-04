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
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/SENERGY-Platform/go-service-base/srv-info-hdl"
	"github.com/SENERGY-Platform/go-service-base/struct-logger/attributes"
	sb_util "github.com/SENERGY-Platform/go-service-base/util"
	"github.com/SENERGY-Platform/reporting-service/pkg/api"
	"github.com/SENERGY-Platform/reporting-service/pkg/apis/jsreport"
	"github.com/SENERGY-Platform/reporting-service/pkg/config"
	"github.com/SENERGY-Platform/reporting-service/pkg/report_engine"
	"github.com/SENERGY-Platform/reporting-service/pkg/util"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var Version = "{version}"

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

	jobRetention, err := time.ParseDuration(cfg.ReportJobRetention)
	if err != nil {
		util.Logger.Error("invalid report job retention", "error", err)
		ec = 1
		return
	}

	if err = report_engine.InitDB(cfg.MongoUrl, cfg.MongoDatabase); err != nil {
		util.Logger.Error("error connecting to database", "error", err)
		ec = 1
		return
	}
	defer report_engine.CloseDB()

	if err = report_engine.EnsureIndexes(jobRetention); err != nil {
		util.Logger.Error("error creating database indexes", "error", err)
		ec = 1
		return
	}

	httpHandler, err := api.CreateServer(cfg, client)
	if err != nil {
		util.Logger.Error("error creating http engine", "error", err)
		ec = 1
		return
	}

	bindAddress := ":" + strconv.FormatInt(int64(cfg.ServerPort), 10)

	if cfg.Debug {
		bindAddress = "127.0.0.1:" + strconv.FormatInt(int64(cfg.ServerPort), 10)
	}

	httpServer := &http.Server{
		Addr:    bindAddress,
		Handler: httpHandler}

	ctx, cf := context.WithCancel(context.Background())
	defer cf()

	go func() {
		util.Wait(ctx, util.Logger, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
		cf()
	}()

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		util.Logger.Info("starting prometheus metrics on :2112/metrics")
		if err := http.ListenAndServe(":2112", nil); err != nil {
			util.Logger.Error("metrics server exited", "error", err)
		}
	}()

	wg := &sync.WaitGroup{}

	wg.Add(4)

	// Every goroutine below keeps its error local. They used to share the err of
	// main, which raced as soon as two of them failed at the same time.
	go func() {
		defer wg.Done()
		util.Logger.Info("init scheduler")
		schedErr := client.RunScheduler(ctx)

		if errors.Is(schedErr, context.Canceled) || errors.Is(schedErr, context.DeadlineExceeded) {
			util.Logger.Info("scheduler exited normally")
			return
		}

		if schedErr != nil {
			util.Logger.Error("could not start scheduler", "error", schedErr)
			ec = 1
			cf()
			return
		}
	}()

	go func() {
		defer wg.Done()
		workerErr := client.RunJobWorkers(ctx)

		if errors.Is(workerErr, context.Canceled) || errors.Is(workerErr, context.DeadlineExceeded) {
			util.Logger.Info("report job workers exited normally")
			return
		}

		if workerErr != nil {
			util.Logger.Error("could not start report job workers", "error", workerErr)
			ec = 1
			cf()
			return
		}
	}()

	go func() {
		defer wg.Done()
		util.Logger.Info("starting http server")
		if serveErr := httpServer.ListenAndServe(); !errors.Is(serveErr, http.ErrServerClosed) {
			util.Logger.Error("starting server failed", attributes.ErrorKey, serveErr)
			ec = 1
			cf()
			return
		}
	}()

	go func() {
		defer wg.Done()
		<-ctx.Done()
		util.Logger.Info("stopping http server")
		ctxWt, cf2 := context.WithTimeout(context.Background(), time.Second*5)
		defer cf2()
		if shutdownErr := httpServer.Shutdown(ctxWt); shutdownErr != nil {
			util.Logger.Error("stopping server failed", attributes.ErrorKey, shutdownErr)
			ec = 1
		} else {
			util.Logger.Info("http server stopped")
		}
	}()

	wg.Wait()
}
