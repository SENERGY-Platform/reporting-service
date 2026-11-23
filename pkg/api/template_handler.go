/*
 * Copyright 2026 InfAI (CC SES)
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

	"github.com/SENERGY-Platform/reporting-service/lib"
	"github.com/SENERGY-Platform/reporting-service/pkg/apis/jsreport"
	"github.com/SENERGY-Platform/reporting-service/pkg/report_engine"
	"github.com/SENERGY-Platform/reporting-service/pkg/util"
	"github.com/SENERGY-Platform/service-commons/pkg/jwt"
	"github.com/gin-gonic/gin"
)

const (
	MessageTemplatesNotConfigured = "writing templates is not configured"
	MessageTemplateFolder         = "template folder not readable"
	MessageNoReportGroup          = "the account has no report group"
	MessageForbidden              = "report engine denied access"
	MessageTemplateNotFound       = "template not found"
	MessageRoleMissing            = "the template author role is missing"
	MessageNoFolderWrite          = "no write access to the template folder"
	MessageNoTemplateWrite        = "no write access to the template"
	MessageOutsideFolder          = "only templates in the template folder can be changed"
)

// allowTemplateAuthor answers the request itself and returns false unless writing
// templates is configured and the caller's token carries the author realm role.
func allowTemplateAuthor(c *gin.Context, reportingClient report_engine.Client) bool {
	if reportingClient.Config == nil || !reportingClient.Config.Templates.Enabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": MessageTemplatesNotConfigured})
		return false
	}
	token, err := jwt.Parse(c.GetHeader(HeaderAuthorization))
	if err != nil {
		util.Logger.Error("could not read the roles of the caller", "error", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return false
	}
	if !token.HasRole(reportingClient.Config.Templates.AuthorRole) {
		c.JSON(http.StatusForbidden, gin.H{"error": MessageRoleMissing})
		return false
	}
	return true
}

// templateFailed turns an error of the report engine into a status the client can act
// on and logs the engine's own message, which is never the authorization header.
// forbidden is the sentence for a 403 of the engine, so the caller can tell a missing
// role, a folder it cannot read and a missing right to write apart.
func templateFailed(c *gin.Context, message string, forbidden string, err error) {
	util.Logger.Error(message, "error", err)
	var apiErr *jsreport.APIError
	var rejected *report_engine.FiguresRejectedError
	detail := ""
	if errors.As(err, &apiErr) {
		detail = apiErr.Message
	}
	switch {
	case errors.Is(err, lib.ErrUnauthorized):
		c.JSON(http.StatusUnauthorized, gin.H{"error": MessageUnauthorized})
	case errors.Is(err, lib.ErrTemplateFolderAccess):
		c.JSON(http.StatusForbidden, gin.H{"error": MessageTemplateFolder})
	case errors.Is(err, lib.ErrNoReportGroup):
		c.JSON(http.StatusForbidden, gin.H{"error": MessageNoReportGroup})
	case errors.Is(err, lib.ErrOutsideTemplateFolder):
		c.JSON(http.StatusForbidden, gin.H{"error": MessageOutsideFolder})
	case errors.Is(err, lib.ErrForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": forbidden})
	case errors.Is(err, lib.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": MessageTemplateNotFound})
	case errors.Is(err, lib.ErrConflict):
		c.JSON(http.StatusConflict, gin.H{"error": withDetail("a template or data with this name already exists", detail)})
	case errors.Is(err, lib.ErrRejected):
		c.JSON(http.StatusBadRequest, gin.H{"error": withDetail("the report engine rejected the template", detail)})
	case errors.As(err, &rejected):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, report_engine.ErrPreviewData):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
	default:
		_ = c.Error(errors.New(MessageSomethingWrong))
	}
}

func withDetail(message string, detail string) string {
	if detail == "" {
		return message
	}
	return message + ": " + detail
}

func invalidInput(c *gin.Context, err error) {
	util.Logger.Error(MessageParseError, "error", err)
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}

// postTemplate godoc
// @Summary Create template
// @Description	Creates a handlebars template in the caller's group folder (the subfolder of TEMPLATE_FOLDER named like the token's TEMPLATE_GROUP_CLAIM), with a data entity holding the sample data, using the caller's token. Needs the realm role from TEMPLATE_AUTHOR_ROLE, 404 while TEMPLATE_FOLDER or the role is not configured.
// @Tags Template
// @Accept json
// @Produce json
// @Param template body lib.TemplateInput true "Template"
// @Success	201 {object} lib.Template
// @Failure	400 {string} str
// @Failure	401 {string} str
// @Failure	403 {string} str
// @Failure	404 {string} str
// @Failure	409 {string} str
// @Failure	500 {string} str
// @Router /templates [post]
func postTemplate(reportingClient report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodPost, "/templates", func(c *gin.Context) {
		if !allowTemplateAuthor(c, reportingClient) {
			return
		}
		var input lib.TemplateInput
		if err := decodeBody(c, &input, true); err != nil {
			invalidInput(c, err)
			return
		}
		if err := validateTemplateInput(input); err != nil {
			invalidInput(c, err)
			return
		}
		template, err := reportingClient.CreateTemplate(input, c.GetHeader(HeaderAuthorization))
		if err != nil {
			templateFailed(c, "could not create template", MessageNoFolderWrite, err)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"data": template})
	}
}

// putTemplate godoc
// @Summary Update template
// @Description	Replaces name, recipe, content, helpers and sample data of a template in the caller's group folder with the caller's token. A template elsewhere is a 403. Nothing is changed unless the name is free and both writes can be made. Needs the realm role from TEMPLATE_AUTHOR_ROLE.
// @Tags Template
// @Accept json
// @Produce json
// @Param id path string true "Template ID"
// @Param template body lib.TemplateInput true "Template"
// @Success	200 {object} lib.Template
// @Failure	400 {string} str
// @Failure	401 {string} str
// @Failure	403 {string} str
// @Failure	404 {string} str
// @Failure	409 {string} str
// @Failure	500 {string} str
// @Router /templates/:id [put]
func putTemplate(reportingClient report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodPut, "/templates/:id", func(c *gin.Context) {
		if !allowTemplateAuthor(c, reportingClient) {
			return
		}
		var input lib.TemplateInput
		if err := decodeBody(c, &input, true); err != nil {
			invalidInput(c, err)
			return
		}
		if err := validateTemplateInput(input); err != nil {
			invalidInput(c, err)
			return
		}
		id := c.Param("id")
		template, err := reportingClient.UpdateTemplate(id, input, c.GetHeader(HeaderAuthorization))
		if err != nil {
			templateFailed(c, "could not update template "+id, MessageNoTemplateWrite, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": template})
	}
}

// deleteTemplate godoc
// @Summary Delete template
// @Description	Deletes a template in the caller's group folder and its sample data with the caller's token. A template elsewhere is a 403. Needs the realm role from TEMPLATE_AUTHOR_ROLE.
// @Tags Template
// @Param id path string true "Template ID"
// @Success	204 {string} str
// @Failure	401 {string} str
// @Failure	403 {string} str
// @Failure	404 {string} str
// @Failure	500 {string} str
// @Router /templates/:id [delete]
func deleteTemplate(reportingClient report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodDelete, "/templates/:id", func(c *gin.Context) {
		if !allowTemplateAuthor(c, reportingClient) {
			return
		}
		id := c.Param("id")
		if err := reportingClient.DeleteTemplate(id, c.GetHeader(HeaderAuthorization)); err != nil {
			templateFailed(c, "could not delete template "+id, MessageNoTemplateWrite, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// postTemplatePreview godoc
// @Summary Preview a template draft
// @Description	Renders a draft or a stored template with report data, which is resolved like in a real run including figuresQuery. Nothing is stored and no job is created. A stored template is rendered by its id, so its engine and settings apply as in a run, and without data its sample data is used. The answer is a PNG of the top of the page in a viewport of 1200 by 1600 pixels, not the whole page, or with as=file the file of the template's own recipe. Needs the realm role from TEMPLATE_AUTHOR_ROLE.
// @Tags Template
// @Accept json
// @Produce png
// @Param preview body lib.TemplatePreviewRequest true "Draft or template id, and the report data"
// @Param as query string false "file renders in the recipe of the template instead of as an image"
// @Success	200
// @Failure	400 {string} str
// @Failure	401 {string} str
// @Failure	403 {string} str
// @Failure	404 {string} str
// @Failure	422 {string} str
// @Failure	500 {string} str
// @Router /templates/preview [post]
func postTemplatePreview(reportingClient report_engine.Client) (string, string, gin.HandlerFunc) {
	return http.MethodPost, "/templates/preview", func(c *gin.Context) {
		if !allowTemplateAuthor(c, reportingClient) {
			return
		}
		as := c.Query("as")
		if as != "" && as != "file" {
			invalidInput(c, errors.New("as must be empty or file"))
			return
		}
		var request lib.TemplatePreviewRequest
		if err := decodeBody(c, &request, false); err != nil {
			invalidInput(c, err)
			return
		}
		if err := validatePreviewRequest(request); err != nil {
			invalidInput(c, err)
			return
		}
		content, contentType, err := reportingClient.PreviewTemplate(request, as == "file", c.GetHeader(HeaderAuthorization))
		if err != nil {
			templateFailed(c, "could not render the template preview", MessageForbidden, err)
			return
		}
		c.Data(http.StatusOK, contentType, content)
	}
}
