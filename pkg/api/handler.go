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

package api

import (
	"errors"
	"net/http"
	"os"
	"strconv"

	"github.com/SENERGY-Platform/reporting-service/lib"
	"github.com/SENERGY-Platform/reporting-service/pkg/report_engine"
	"github.com/SENERGY-Platform/reporting-service/pkg/util"
	"github.com/gin-gonic/gin"
)

// failed answers a request that could not be served. A token the report engine
// rejected gets its own status, because that is a configuration problem a client
// can act on rather than a generic failure it can only retry.
func failed(c *gin.Context, message string, err error) {
	util.Logger.Error(message, "error", err)
	if errors.Is(err, lib.ErrUnauthorized) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": MessageUnauthorized})
		return
	}
	_ = c.Error(errors.New(MessageSomethingWrong))
}

// getTemplates godoc
// @Summary Get all templates
// @Description	Gets all templates
// @Tags Template
// @Produce json
// @Success	200 {array} lib.Template
// @Failure	401 {string} str
// @Failure	500 {string} str
// @Router /templates [get]
func getTemplates(reportingClient report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodGet, "/templates", func(c *gin.Context) {
		templates, err := reportingClient.GetTemplates(c.GetHeader(HeaderAuthorization))
		if err != nil {
			failed(c, "could not get templates", err)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"data": templates,
		})
	}
}

// getTemplate godoc
// @Summary Get template by id
// @Description	Gets template by id
// @Tags Template
// @Produce json
// @Param id path string true "Template ID"
// @Success	200 {object} lib.Template
// @Failure	401 {string} str
// @Failure	500 {string} str
// @Router /templates/:id [get]
func getTemplate(reportingClient report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodGet, "/templates/:id", func(c *gin.Context) {
		id := c.Param("id")
		template, err := reportingClient.GetTemplateById(id, c.GetHeader(HeaderAuthorization))
		if err != nil {
			failed(c, "could not get template "+id, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"data": template,
		})
	}
}

// getTemplatePreview godoc
// @Summary Get template preview by id
// @Description	Gets template preview by id
// @Tags Template
// @Produce json
// @Param id path string true "Template ID"
// @Success	200
// @Failure	401 {string} str
// @Failure	500 {string} str
// @Router /templates/preview/:id [get]
func getTemplatePreview(reportingClient report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodGet, "/templates/preview/:id", func(c *gin.Context) {
		id := c.Param("id")
		content, contentType, _, err := reportingClient.GetTemplatePreviewById(id, c.GetHeader(HeaderAuthorization))
		if err != nil {
			failed(c, "could not get template preview "+id, err)
			return
		}
		c.Data(http.StatusOK, contentType, content)
	}
}

// postReportCreate godoc
// @Summary Queue report file creation
// @Description	Queues creation of a report file. Returns the id of the report and the id of the job that tracks the creation, which can be polled at /report/job/:jobId.
// @Tags Report
// @Produce json
// @Param report body lib.Report true "Report"
// @Success	202 {object} map[string]string
// @Failure	500 {string} str
// @Router /report/create [post]
func postReportCreate(reportingClient report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodPost, "/report/create", func(c *gin.Context) {
		var request lib.Report
		if err := c.ShouldBindJSON(&request); err != nil {
			util.Logger.Error(MessageParseError, "error", err)
			_ = c.Error(errors.New(MessageSomethingWrong))
			return
		}
		reportId, jobId, err := reportingClient.EnqueueReportFileCreation(request, c.GetHeader(HeaderAuthorization))
		if err != nil {
			util.Logger.Error("could not queue report file creation", "error", err)
			_ = c.Error(errors.New(MessageSomethingWrong))
			return
		}
		c.JSON(http.StatusAccepted, gin.H{
			"id":    reportId,
			"jobId": jobId,
		})
	}
}

// getReportJob godoc
// @Summary Get report job by id
// @Description	Gets the status of a queued report file creation
// @Tags Report
// @Produce json
// @Param jobId path string true "Job ID"
// @Success	200 {object} lib.ReportJob
// @Failure	404 {string} str
// @Failure	500 {string} str
// @Router /report/job/:jobId [get]
func getReportJob(reportingClient report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodGet, "/report/job/:jobId", func(c *gin.Context) {
		jobId := c.Param("jobId")
		job, err := reportingClient.GetReportJob(jobId, c.GetHeader(HeaderAuthorization))
		if errors.Is(err, report_engine.ErrJobNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": MessageJobNotFound})
			return
		}
		if err != nil {
			util.Logger.Error("could not get report job "+jobId, "error", err)
			_ = c.Error(errors.New(MessageSomethingWrong))
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"data": job,
		})
	}
}

// getReportJobs godoc
// @Summary Get report jobs
// @Description	Gets the most recent report jobs of the user, newest first
// @Tags Report
// @Produce json
// @Param reportId query string false "only jobs of this report"
// @Param limit query int false "maximum number of jobs to return"
// @Success	200 {array} lib.ReportJob
// @Failure	400 {string} str
// @Failure	500 {string} str
// @Router /report/job [get]
func getReportJobs(reportingClient report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodGet, "/report/job", func(c *gin.Context) {
		var limit int64
		if raw := c.Query("limit"); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || parsed < 1 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
				return
			}
			limit = parsed
		}
		jobs, err := reportingClient.GetReportJobs(c.GetHeader(HeaderAuthorization), c.Query("reportId"), limit)
		if err != nil {
			util.Logger.Error("could not get report jobs", "error", err)
			_ = c.Error(errors.New(MessageSomethingWrong))
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"data": jobs,
		})
	}
}

// postReport godoc
// @Summary Create report model
// @Description	Creates report model
// @Tags Report
// @Produce json
// @Param report body lib.Report true "Report"
// @Success	200 {string} str
// @Failure	500 {string} str
// @Router /report [post]
func postReport(reportingClient report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodPost, "/report", func(c *gin.Context) {
		var request lib.Report
		if err := c.ShouldBindJSON(&request); err != nil {
			util.Logger.Error(MessageParseError, "error", err)
			_ = c.Error(errors.New(MessageSomethingWrong))
			return
		}
		_, err := reportingClient.SaveReportModel(request, c.GetHeader(HeaderAuthorization))
		if err != nil {
			util.Logger.Error("could not save report", "error", err)
			_ = c.Error(errors.New(MessageSomethingWrong))
			return
		}
		c.Status(http.StatusOK)
	}
}

// putReport godoc
// @Summary Update report model
// @Description	Updates report model
// @Tags Report
// @Produce json
// @Param report body lib.Report true "Report"
// @Success	200 {string} str
// @Failure	500 {string} str
// @Router /report [put]
func putReport(reportingClient report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodPut, "/report", func(c *gin.Context) {
		var request lib.Report
		if err := c.ShouldBindJSON(&request); err != nil {
			util.Logger.Error(MessageParseError, "error", err)
			_ = c.Error(errors.New(MessageSomethingWrong))
			return
		}
		err := reportingClient.UpdateReportModel(request, c.GetHeader(HeaderAuthorization))
		if err != nil {
			util.Logger.Error("could not update report", "error", err)
			_ = c.Error(errors.New(MessageSomethingWrong))
			return
		}
		c.Status(http.StatusOK)
	}
}

// getReports godoc
// @Summary Get all reports
// @Description	Gets all reports
// @Tags Report
// @Produce json
// @Success	200 {array} lib.Report
// @Failure	500 {string} str
// @Router /report [get]
func getReports(reportingClient report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodGet, "/report", func(c *gin.Context) {
		args := c.Request.URL.Query()
		reports, err := reportingClient.GetReportModels(c.GetHeader(HeaderAuthorization), args, false)
		if err != nil {
			util.Logger.Error("could not get reports", "error", err)
			_ = c.Error(errors.New(MessageSomethingWrong))
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"data": reports,
		})
	}
}

// getReport godoc
// @Summary Get report by id
// @Description	Gets report by id
// @Tags Report
// @Produce json
// @Param id path string true "Report ID"
// @Success	200 {object} lib.Report
// @Failure	500 {string} str
// @Router /report/:id [get]
func getReport(reportingClient report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodGet, "/report/:id", func(c *gin.Context) {
		id := c.Param("id")
		report, err := reportingClient.GetReportModel(id, c.GetHeader(HeaderAuthorization))
		if err != nil {
			util.Logger.Error("could not get report "+id, "error", err)
			_ = c.Error(errors.New(MessageSomethingWrong))
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"data": report,
		})
	}
}

// deleteReport godoc
// @Summary Delete report by id
// @Description	Deletes report by id
// @Tags Report
// @Success	204 {string} str
// @Failure	500 {string} str
// @Router /report/:id [delete]
func deleteReport(reportingClient report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodDelete, "/report/:id", func(c *gin.Context) {
		id := c.Param("id")
		err := reportingClient.DeleteReport(id, c.GetHeader(HeaderAuthorization), false)
		if err != nil {
			util.Logger.Error("could not delete reports", "error", err)
			_ = c.Error(errors.New(MessageSomethingWrong))
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// getReportFile godoc
// @Summary Get report file by id
// @Description	Gets report file by id
// @Tags Report
// @Produce json
// @Param reportId path string true "Report ID"
// @Param fileId path string true "File ID"
// @Success	200
// @Failure	401 {string} str
// @Failure	500 {string} str
// @Router /report/file/:reportId/:fileId [get]
func getReportFile(reportingClient report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodGet, "/report/file/:reportId/:fileId", func(c *gin.Context) {
		reportId := c.Param("reportId")
		fileId := c.Param("fileId")
		content, contentType, _, err := reportingClient.DownloadReportFile(reportId, fileId, c.GetHeader(HeaderAuthorization))
		if err != nil {
			failed(c, "could not get report file "+fileId, err)
			return
		}
		c.Data(http.StatusOK, contentType, content)
	}
}

// deleteReportFile godoc
// @Summary Delete report file by id
// @Description	Deletes report file by id
// @Tags Report
// @Success	204 {string} str
// @Failure	401 {string} str
// @Failure	500 {string} str
// @Router /report/file/:reportId/:fileId [delete]
func deleteReportFile(reportingClient report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodDelete, "/report/file/:reportId/:fileId", func(c *gin.Context) {
		reportId := c.Param("reportId")
		fileId := c.Param("fileId")
		err := reportingClient.DeleteCreatedReportFile(reportId, fileId, c.GetHeader(HeaderAuthorization))
		if err != nil {
			failed(c, "could not delete report file "+fileId, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func getHealthCheckH(_ report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodGet, HealthCheckPath, func(c *gin.Context) {
		c.Status(http.StatusOK)
	}
}

func getSwaggerDocH(_ report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodGet, "/doc", func(gc *gin.Context) {
		if _, err := os.Stat("docs/swagger.json"); err != nil {
			_ = gc.Error(err)
			return
		}
		gc.Header("Content-Type", gin.MIMEJSON)
		gc.File("docs/swagger.json")
	}
}
