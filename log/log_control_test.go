/*
 * Copyright 2025 The Go-Spring Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package log

import (
	"testing"

	"go-spring.org/stdlib/testing/assert"
)

func TestLoggers(t *testing.T) {
	defer Destroy()

	// Before the first refresh no loggers are configured.
	assert.Slice(t, Loggers()).Empty()

	// After a successful refresh the configured loggers are reported with
	// their effective minimum level, sorted by name.
	err := RefreshConfig(ReadTestConfig())
	assert.Error(t, err).Nil()
	assert.Slice(t, Loggers()).Equal([]LoggerInfo{
		{Name: "myLogger", Level: "TRACE"},
		{Name: RootLoggerName, Level: "WARN"},
	})
}

func TestAvailableLevels(t *testing.T) {
	assert.Slice(t, AvailableLevels()).Equal([]string{
		"TRACE", "DEBUG", "INFO", "WARN", "ERROR", "PANIC", "FATAL",
	})
}
