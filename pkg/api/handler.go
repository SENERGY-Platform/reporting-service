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

	"github.com/SENERGY-Platform/reporting-service/lib"
	"github.com/SENERGY-Platform/reporting-service/pkg/report_engine"
	"github.com/SENERGY-Platform/reporting-service/pkg/util"
	"github.com/gin-gonic/gin"
)

// getTemplates godoc
// @Summary Get all templates
// @Description	Gets all templates
// @Tags Template
// @Produce json
// @Success	200 {array} lib.Template
// @Failure	500 {string} str
// @Router /templates [get]
func getTemplates(reportingClient report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodGet, "/templates", func(c *gin.Context) {
		templates, err := reportingClient.GetTemplates(c.GetHeader(HeaderAuthorization))
		if err != nil {
			util.Logger.Error("could not get templates", "error", err)
			_ = c.Error(errors.New(MessageSomethingWrong))
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
// @Failure	500 {string} str
// @Router /templates/:id [get]
func getTemplate(reportingClient report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodGet, "/templates/:id", func(c *gin.Context) {
		id := c.Param("id")
		template, err := reportingClient.GetTemplateById(id, c.GetHeader(HeaderAuthorization))
		if err != nil {
			util.Logger.Error("could not get template "+id, "error", err)
			_ = c.Error(errors.New(MessageSomethingWrong))
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
// @Failure	500 {string} str
// @Router /templates/preview/:id [get]
func getTemplatePreview(reportingClient report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodGet, "/templates/preview/:id", func(c *gin.Context) {
		id := c.Param("id")
		content, contentType, _, err := reportingClient.GetTemplatePreviewById(id, c.GetHeader(HeaderAuthorization))
		if err != nil {
			util.Logger.Error("could not get template preview"+id, "error", err)
			_ = c.Error(errors.New(MessageSomethingWrong))
			return
		}
		c.Data(http.StatusOK, contentType, content)
	}
}

// postReportCreate godoc
// @Summary Create report file
// @Description	Creates report file
// @Tags Report
// @Produce json
// @Param report body lib.Report true "Report"
// @Success	200 {string} str
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
		result, _, err := reportingClient.CreateReportFile(request, c.GetHeader(HeaderAuthorization))
		if err != nil {
			util.Logger.Error("could not create report file", "error", err)
			_ = c.Error(errors.New(MessageSomethingWrong))
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"id": result.Id,
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
// @Failure	500 {string} str
// @Router /report/file/:reportId/:fileId [get]
func getReportFile(reportingClient report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodGet, "/report/file/:reportId/:fileId", func(c *gin.Context) {
		reportId := c.Param("reportId")
		fileId := c.Param("fileId")
		content, contentType, _, err := reportingClient.DownloadReportFile(reportId, fileId, c.GetHeader(HeaderAuthorization))
		if err != nil {
			util.Logger.Error("could not get report file "+fileId, "error", err)
			_ = c.Error(errors.New(MessageSomethingWrong))
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
// @Failure	500 {string} str
// @Router /report/file/:reportId/:fileId [delete]
func deleteReportFile(reportingClient report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodDelete, "/report/file/:reportId/:fileId", func(c *gin.Context) {
		reportId := c.Param("reportId")
		fileId := c.Param("fileId")
		err := reportingClient.DeleteCreatedReportFile(reportId, fileId, c.GetHeader(HeaderAuthorization))
		if err != nil {
			util.Logger.Error("could not delete report file "+fileId, "error", err)
			_ = c.Error(errors.New(MessageSomethingWrong))
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
