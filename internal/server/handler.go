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
	"github.com/gin-gonic/gin"
	"net/http"
	"report-service/internal/report_engine"
)

func startAPI(reportingClient *report_engine.Client) {
	r := gin.Default()
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})
	r.GET("/templates", func(c *gin.Context) {
		templates, err := reportingClient.GetTemplates()
		if err != nil {
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"data": templates,
		})
	})
	r.GET("/templates/:id", func(c *gin.Context) {
		id := c.Param("id")
		template, err := reportingClient.GetTemplateById(id)
		if err != nil {
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"data": template,
		})
	})
	err := r.Run("127.0.0.1:8080")
	if err == nil {
		fmt.Printf("Starting api server failed: %s \n", err)
	}
}
