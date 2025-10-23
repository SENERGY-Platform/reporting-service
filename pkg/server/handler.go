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
	"fmt"
	"log"
	"net/http"

	"github.com/SENERGY-Platform/reporting-service/pkg/config"
	"github.com/SENERGY-Platform/reporting-service/pkg/models"

	"github.com/SENERGY-Platform/reporting-service/pkg/report_engine"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func StartAPI(reportingClient *report_engine.Client, cfg config.Config) {
	if !cfg.Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.Default()
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "DELETE", "OPTIONS", "PUT"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))
	prefix := r.Group(cfg.URLPrefix)
	prefix.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})

	prefix.GET("/templates", func(c *gin.Context) {
		authString := c.GetHeader("Authorization")
		templates, err := reportingClient.GetTemplates(authString)
		if err != nil {
			log.Println(err)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"data": templates,
		})
	})

	prefix.GET("/templates/:id", func(c *gin.Context) {
		id := c.Param("id")
		authString := c.GetHeader("Authorization")
		template, err := reportingClient.GetTemplateById(id, authString)
		if err != nil {
			log.Println(err)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"data": template,
		})
	})

	prefix.GET("/templates/preview/:id", func(c *gin.Context) {
		id := c.Param("id")
		authString := c.GetHeader("Authorization")
		content, contentType, _, err := reportingClient.GetTemplatePreviewById(id, authString)
		if err != nil {
			log.Println(err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Data(http.StatusOK, contentType, content)
	})

	prefix.POST("/report/create", func(c *gin.Context) {
		var request models.Report
		authString := c.GetHeader("Authorization")
		if err := c.ShouldBindJSON(&request); err != nil {
			log.Println(err.Error())
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		result, _, err := reportingClient.CreateReportFile(request, authString)
		if err != nil {
			log.Println(err.Error())
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"id": result.Id,
		})
	})

	prefix.POST("/report", func(c *gin.Context) {
		var request models.Report
		authString := c.GetHeader("Authorization")
		if err := c.ShouldBindJSON(&request); err != nil {
			log.Println(err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		_, err := reportingClient.SaveReportModel(request, authString)
		if err != nil {
			log.Println(err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusOK)
	})

	prefix.PUT("/report", func(c *gin.Context) {
		var request models.Report
		authString := c.GetHeader("Authorization")
		if err := c.ShouldBindJSON(&request); err != nil {
			log.Println(err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		err := reportingClient.UpdateReportModel(request, authString)
		if err != nil {
			log.Println(err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusOK)
	})

	prefix.GET("/report", func(c *gin.Context) {
		args := c.Request.URL.Query()
		authString := c.GetHeader("Authorization")
		reports, err := reportingClient.GetReportModels(authString, args, false)
		if err != nil {
			log.Println(err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"data": reports,
		})
	})

	prefix.DELETE("/report/:id", func(c *gin.Context) {
		id := c.Param("id")
		authString := c.GetHeader("Authorization")
		err := reportingClient.DeleteReport(id, authString, false)
		if err != nil {
			log.Println(err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	})

	prefix.GET("/report/:id", func(c *gin.Context) {
		id := c.Param("id")
		authString := c.GetHeader("Authorization")
		report, err := reportingClient.GetReportModel(id, authString)
		if err != nil {
			log.Println(err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"data": report,
		})
	})

	prefix.GET("/report/file/:reportId/:fileId", func(c *gin.Context) {
		reportId := c.Param("reportId")
		fileId := c.Param("fileId")
		authString := c.GetHeader("Authorization")
		content, contentType, _, err := reportingClient.DownloadReportFile(reportId, fileId, authString)
		if err != nil {
			log.Println(err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Data(http.StatusOK, contentType, content)
	})

	prefix.DELETE("/report/file/:reportId/:fileId", func(c *gin.Context) {
		reportId := c.Param("reportId")
		fileId := c.Param("fileId")
		authString := c.GetHeader("Authorization")
		err := reportingClient.DeleteCreatedReportFile(reportId, fileId, authString)
		if err != nil {
			log.Println(err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	})

	var err error
	if !cfg.Debug {
		err = r.Run()
	} else {
		err = r.Run("127.0.0.1:8080")
	}
	if err == nil {
		fmt.Printf("Starting api server failed: %s \n", err)
	}
}
