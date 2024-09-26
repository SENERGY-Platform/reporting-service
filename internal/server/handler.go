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
	"report-service/internal/helper"
	"report-service/internal/report_engine"
	"strconv"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func startAPI(reportingClient *report_engine.Client) {
	DEBUG, err := strconv.ParseBool(helper.GetEnv("DEBUG", "false"))
	if err != nil {
		log.Print("Error loading debug value")
		DEBUG = false
	}
	if !DEBUG {
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

	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})

	r.GET("/templates", func(c *gin.Context) {
		authString := c.GetHeader("Authorization")
		templates, err := reportingClient.GetTemplates(authString)
		if err != nil {
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"data": templates,
		})
	})

	r.GET("/templates/:id", func(c *gin.Context) {
		id := c.Param("id")
		authString := c.GetHeader("Authorization")
		template, err := reportingClient.GetTemplateById(id, authString)
		if err != nil {
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"data": template,
		})
	})

	r.POST("/report/create", func(c *gin.Context) {
		var request report_engine.Report
		authString := c.GetHeader("Authorization")
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		result, err := reportingClient.CreateReport(request, authString)
		if err != nil {
			log.Println(err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"id": result.Id,
		})
	})

	r.POST("/report", func(c *gin.Context) {
		var request report_engine.Report
		authString := c.GetHeader("Authorization")
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		_, err := reportingClient.SaveReport(request, authString)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusOK)
	})

	r.PUT("/report", func(c *gin.Context) {
		var request report_engine.Report
		authString := c.GetHeader("Authorization")
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		err := reportingClient.UpdateReport(request, authString)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusOK)
	})

	r.GET("/report", func(c *gin.Context) {
		args := c.Request.URL.Query()
		authString := c.GetHeader("Authorization")
		reports, err := reportingClient.GetReports(authString, args, false)
		if err != nil {
			log.Println(err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"data": reports,
		})
	})

	r.DELETE("/report/:id", func(c *gin.Context) {
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

	r.GET("/report/:id", func(c *gin.Context) {
		id := c.Param("id")
		authString := c.GetHeader("Authorization")
		report, err := reportingClient.GetReport(id, authString)
		if err != nil {
			log.Println(err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"data": report,
		})
	})

	r.GET("/report/file/:reportId/:fileId", func(c *gin.Context) {
		reportId := c.Param("reportId")
		fileId := c.Param("fileId")
		authString := c.GetHeader("Authorization")
		content, contentType, err := reportingClient.DownloadReportFile(reportId, fileId, authString)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Data(http.StatusOK, contentType, content)
	})

	r.DELETE("/report/file/:reportId/:fileId", func(c *gin.Context) {
		reportId := c.Param("reportId")
		fileId := c.Param("fileId")
		authString := c.GetHeader("Authorization")
		err := reportingClient.DeleteCreatedReportFile(reportId, fileId, authString)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	})

	if !DEBUG {
		err = r.Run()
	} else {
		err = r.Run("127.0.0.1:8080")
	}
	if err == nil {
		fmt.Printf("Starting api server failed: %s \n", err)
	}
}
