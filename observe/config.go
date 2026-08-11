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

// Package observe is a uniform trace+metric+log observer for client operations
// (cache, database, messaging). Each client starter attaches its instrumentation
// seam — a driver hook, a connection wrapper, a command monitor, a gorm callback
// — to one Observer, so every client emits the same three signals with the same
// vocabulary, behind one verbosity switch. That replaces the per-starter trace-only
// or span-helper-only instrumentation that drifted across the starter family.
//
// All three signals ride the OTel globals (TracerProvider / MeterProvider) that
// starter-otel installs. When starter-otel is absent those globals are no-ops, so
// trace and metric cost almost nothing and change no behavior; the access log
// (the third signal) always emits through the project log package, gated only by
// LogConfig.Level.
package observe

// LogConfig controls the per-operation access log emitted by an Observer. It is
// bound per client instance under spring.<client>.<name>.observability.log.* by
// each starter's own Config (mirroring gin's AccessLogConfig.PayloadConfig).
type LogConfig struct {
	// Level controls access-log detail:
	//
	//   "off"      - no access log. Trace and metric still emit; only the log
	//                signal is silenced (e.g. for very high-volume clients).
	//   "brief"    - one record per operation carrying system, operation, status,
	//                duration, and error. The default: enough to spot slow/failing
	//                ops without pulling arguments.
	//   "detailed" - brief plus the operation argument (command name + key, sql
	//                statement, cache key, message topic, ...), bounded by
	//                MaxArgBytes so a large statement or payload can't flood the
	//                log. Use for troubleshooting.
	Level string `value:"${level:=brief}"`

	// MaxArgBytes caps how many bytes of the operation argument are captured in
	// "detailed" mode (per record). Defaults to 512, enough for a typical command
	// key or short statement without letting a runaway payload exhaust memory or
	// blow up the log line. Set higher only when you need fuller statements.
	MaxArgBytes int `value:"${maxArgBytes:=512}"`

	// SkipOps suppresses all three signals (span, metric, access log) for the
	// listed operations, so a chatty or noisy operation — a Redis PING, a health
	// probe, a hot cache key — does not flood the backends. An entry matches the
	// operation name passed to Observer.Start (e.g. "PING", "ping", "health").
	SkipOps []string `value:"${skipOps:=}"`
}

// level constants — compared as strings so a starter can bind Level straight from
// config without an enum parse step.
const (
	levelOff      = "off"
	levelBrief    = "brief"
	levelDetailed = "detailed"

	// DefaultBrief is the default Level value ("brief"), exported so starters
	// that build a LogConfig programmatically (rather than binding from config)
	// reference the same default.
	DefaultBrief = levelBrief
)

// enabled reports whether the access log emits at the configured level.
func (c LogConfig) enabled() bool { return c.Level != levelOff }

// detailed reports whether the operation argument is captured into the log.
func (c LogConfig) detailed() bool { return c.Level == levelDetailed }

// maxArg returns the argument capture bound, defaulting to 512 when unset.
func (c LogConfig) maxArg() int {
	if c.MaxArgBytes > 0 {
		return c.MaxArgBytes
	}
	return 512
}
