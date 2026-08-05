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

package report_engine

import (
	"testing"
	"time"

	snrgyModels "github.com/SENERGY-Platform/models/go/models"
)

func TestParseDuration(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want time.Duration
	}{
		{"reads seconds", "30s", 30 * time.Second},
		{"reads minutes", "15m", 15 * time.Minute},
		{"reads hours", "6h", 6 * time.Hour},
		{"reads days", "7d", 7 * 24 * time.Hour},
		{"reads weeks", "2w", 14 * 24 * time.Hour},
		{"sums up multiple units", "1d12h30m", 36*time.Hour + 30*time.Minute},
		{"keeps a negative value negative", "-5m", -5 * time.Minute},
		{"keeps a negative value negative in a sum", "-1h-30m", -90 * time.Minute},
		{"reads an explicit plus sign", "+5m", 5 * time.Minute},
		{"reads zero", "0s", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseDuration(c.in)
			if err != nil {
				t.Fatalf("ParseDuration(%q) returned an error: %v", c.in, err)
			}
			if got != c.want {
				t.Errorf("ParseDuration(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestParseDurationRejects(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"rejects an empty string", ""},
		{"rejects a missing unit", "15"},
		{"rejects an unknown unit", "15y"},
		{"rejects a missing number", "m"},
		{"rejects a unit that is not a single letter", "12months"},
		{"rejects a fractional number", "1.5h"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ParseDuration(c.in); err == nil {
				t.Errorf("ParseDuration(%q) succeeded, want an error", c.in)
			}
		})
	}
}

func TestHasAttributeWithKey(t *testing.T) {
	attributes := []snrgyModels.Attribute{
		{Key: "shared/nickname", Value: "Zähler Keller"},
		{Key: "other", Value: "x"},
	}
	if !hasAttributeWithKey(attributes, "shared/nickname") {
		t.Error("hasAttributeWithKey did not find a key that is present")
	}
	if hasAttributeWithKey(attributes, "missing") {
		t.Error("hasAttributeWithKey found a key that is not present")
	}
	if hasAttributeWithKey(nil, "shared/nickname") {
		t.Error("hasAttributeWithKey found a key in a nil slice")
	}
}
