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
	"sync"

	"github.com/gin-gonic/gin"
)

// MessagePreviewRunning answers a preview while another one of the same account still renders.
const MessagePreviewRunning = "a preview of this account is still rendering, try again when it is done"

// renderGate lets each account run one preview at a time. jsreport renders for every
// account with a few worker threads, and a template that loops holds one until the
// report timeout, so one account must not be able to take them all.
type renderGate struct {
	mu      sync.Mutex
	running map[string]bool
}

var previews = &renderGate{running: map[string]bool{}}

func (g *renderGate) enter(account string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.running[account] {
		return false
	}
	g.running[account] = true
	return true
}

func (g *renderGate) leave(account string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.running, account)
}

// enterPreview answers the request itself and returns false while the caller's account
// has a preview rendering. Otherwise the caller must call the returned leave when done.
func enterPreview(c *gin.Context) (leave func(), ok bool) {
	account := c.GetString(UserIdKey)
	if !previews.enter(account) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": MessagePreviewRunning})
		return nil, false
	}
	return func() { previews.leave(account) }, true
}
