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

package report_engine

import (
	"errors"
	"strconv"
	"time"

	snrgyModels "github.com/SENERGY-Platform/models/go/models"
)

func ParseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, errors.New("empty duration string")
	}

	var total time.Duration
	var numStr string
	var num int64
	var err error

	for i := 0; i < len(s); {
		// Find the next number
		start := i
		for i < len(s) && (s[i] == '-' || s[i] == '+' || s[i] == '.' || (s[i] >= '0' && s[i] <= '9')) {
			i++
		}
		if i == start {
			return 0, errors.New("invalid duration: " + s)
		}
		numStr = s[start:i]

		// Parse the number
		num, err = strconv.ParseInt(numStr, 10, 64)
		if err != nil {
			return 0, err
		}

		// Get the unit
		if i >= len(s) {
			return 0, errors.New("missing unit in duration: " + s)
		}
		unit := s[i]
		i++

		// Convert to time.Duration
		var d time.Duration
		switch unit {
		case 's':
			d = time.Duration(num) * time.Second
		case 'm':
			d = time.Duration(num) * time.Minute
		case 'h':
			d = time.Duration(num) * time.Hour
		case 'd':
			d = time.Duration(num) * 24 * time.Hour
		case 'w':
			d = time.Duration(num) * 7 * 24 * time.Hour
		default:
			return 0, errors.New("unknown unit " + string(unit) + " in duration " + s)
		}

		// Add to total
		if len(numStr) > 0 && numStr[0] == '-' {
			total -= d
		} else {
			total += d
		}
	}

	return total, nil
}

func hasAttributeWithKey(attributes []snrgyModels.Attribute, key string) bool {
	for _, attr := range attributes {
		if attr.Key == key {
			return true
		}
	}
	return false
}
