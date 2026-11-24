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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SENERGY-Platform/reporting-service/lib"
)

// slowDriver holds the first render until release is closed, every later one returns at once.
type slowDriver struct {
	authoringDriver
	entered chan struct{}
	release chan struct{}
	calls   int
}

func (d *slowDriver) RenderInline(render lib.PreviewRender, data map[string]interface{}, auth string) ([]byte, string, error) {
	d.calls++
	if d.calls == 1 {
		close(d.entered)
		<-d.release
	}
	return d.authoringDriver.RenderInline(render, data, auth)
}

func (d *slowDriver) GetTemplatePreview(string, string) ([]byte, string, string, error) {
	return []byte("PNG"), "image/png", "png", nil
}

func TestAnAccountRendersOnePreviewAtATime(t *testing.T) {
	driver := &slowDriver{authoringDriver: authoringDriver{contentType: "image/png"}, entered: make(chan struct{}), release: make(chan struct{})}
	engine := authoringServer(t, driver, configured())
	body := `{"template":{"recipe":"chrome-pdf","content":"x"},"data":{}}`
	first := bearerWithRoles("gate-u1", testAuthorRole)

	done := make(chan *httptest.ResponseRecorder)
	go func() { done <- send(t, engine, http.MethodPost, "/templates/preview", body, first) }()
	<-driver.entered

	if rec := send(t, engine, http.MethodPost, "/templates/preview", body, first); rec.Code != http.StatusTooManyRequests {
		t.Errorf("second preview of the same account: status = %d, want 429", rec.Code)
	}
	if rec := send(t, engine, http.MethodGet, "/templates/preview/t1", "", first); rec.Code != http.StatusTooManyRequests {
		t.Errorf("stored template preview of the same account: status = %d, want 429", rec.Code)
	}
	if rec := send(t, engine, http.MethodPost, "/templates/preview", body, bearerWithRoles("gate-u2", testAuthorRole)); rec.Code != http.StatusOK {
		t.Errorf("preview of another account: status = %d, want 200", rec.Code)
	}

	close(driver.release)
	if rec := <-done; rec.Code != http.StatusOK {
		t.Fatalf("first preview: status = %d", rec.Code)
	}
	if rec := send(t, engine, http.MethodPost, "/templates/preview", body, first); rec.Code != http.StatusOK {
		t.Errorf("preview after the first finished: status = %d, want 200", rec.Code)
	}
}
