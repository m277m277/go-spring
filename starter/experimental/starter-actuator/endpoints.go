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

package StarterActuator

import (
	"net/http"
	"regexp"
	"runtime/pprof"
	"strings"

	"go-spring.org/log"
	"go-spring.org/stdlib/flatten"
)

// secretKeyRe matches configuration keys whose values are sensitive and must be
// masked before being surfaced to operators. Case-insensitive substring match
// so e.g. "spring.datasource.password" and "auth.access-key" both hit.
var secretKeyRe = regexp.MustCompile(`(?i)(password|passwd|secret|token|credential|api-?key|private-key|access-key)`)

// maskValue redacts a property value when its key names a secret or its value is
// an encrypted placeholder (ENC(...), as produced by the config-encryption
// support). Non-sensitive values pass through unchanged.
func maskValue(key, val string) string {
	if secretKeyRe.MatchString(key) {
		return "******"
	}
	if strings.HasPrefix(val, "ENC(") && strings.HasSuffix(val, ")") {
		return "******"
	}
	return val
}

// loggerEntry is the per-logger detail reported by /loggers.
type loggerEntry struct {
	ConfiguredLevel string `json:"configuredLevel"`
}

// handleLoggers lists the configured loggers with their effective levels, the
// Go analogue of Spring Boot's /actuator/loggers (read-only).
func (s *Server) handleLoggers(w http.ResponseWriter, r *http.Request) {
	loggers := make(map[string]loggerEntry)
	for _, l := range log.Loggers() {
		loggers[l.Name] = loggerEntry{ConfiguredLevel: l.Level}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"loggers": loggers,
	})
}

// handleEnv reports every configuration source with its raw, masked flat
// properties. Sources are listed in priority order (highest first), unmerged, so
// operators see the original data and judge for themselves how it aggregates.
// Values whose keys name secrets or that are ENC(...) placeholders are redacted.
func (s *Server) handleEnv(w http.ResponseWriter, r *http.Request) {
	sources := s.snapshot()
	propertySources := make([]map[string]any, 0, len(sources))
	for _, src := range sources {
		properties := make(map[string]any, len(src.Data))
		for k, v := range src.Data {
			properties[k] = map[string]string{"value": maskValue(k, v)}
		}
		propertySources = append(propertySources, map[string]any{
			"name":       src.Name,
			"properties": properties,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"propertySources": propertySources,
	})
}

// handleConfigProps reports every configuration source as a nested tree, the Go
// analogue of Spring Boot's /actuator/configprops structured view. Sources are
// listed in priority order (highest first), unmerged, and values are masked with
// the same policy as /env.
func (s *Server) handleConfigProps(w http.ResponseWriter, r *http.Request) {
	sources := s.snapshot()
	trees := make([]map[string]any, 0, len(sources))
	for _, src := range sources {
		trees = append(trees, map[string]any{
			"name":   src.Name,
			"config": buildTree(src.Data),
		})
	}
	writeJSON(w, http.StatusOK, trees)
}

// handleThreadDump writes a goroutine stack dump as text/plain — the Go analogue
// of a JVM thread dump. Uses debug detail level 2 for full stacks.
func (s *Server) handleThreadDump(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = pprof.Lookup("goroutine").WriteTo(w, 2)
}

// snapshot returns the current configuration sources, or an empty slice when no
// PropertiesRefresher was injected.
func (s *Server) snapshot() []flatten.Source {
	if s.Config == nil {
		return []flatten.Source{}
	}
	return s.Config.Sources()
}

// buildTree expands dot-separated property keys into a nested map, masking leaf
// values. A path segment that collides with an existing leaf (e.g. both "a" and
// "a.b" present) keeps the leaf and drops the deeper branch, since a malformed
// overlap must not panic an introspection endpoint.
func buildTree(flat map[string]string) map[string]any {
	root := map[string]any{}
	for key, val := range flat {
		segments := strings.Split(key, ".")
		node := root
		ok := true
		for _, seg := range segments[:len(segments)-1] {
			child, exists := node[seg]
			if !exists {
				m := map[string]any{}
				node[seg] = m
				node = m
				continue
			}
			m, isMap := child.(map[string]any)
			if !isMap {
				// A leaf already occupies this segment; skip the branch.
				ok = false
				break
			}
			node = m
		}
		if !ok {
			continue
		}
		leaf := segments[len(segments)-1]
		if _, exists := node[leaf]; !exists {
			node[leaf] = maskValue(key, val)
		}
	}
	return root
}
