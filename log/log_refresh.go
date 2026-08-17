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
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"

	"go-spring.org/log/expr"
	"go-spring.org/stdlib/errutil"
	"go-spring.org/stdlib/flatten"
)

// RootLoggerName defines the reserved name for the root logger.
// This is the default logger used when no specific logger is matched.
const RootLoggerName = "root"

// global maintains all active loggers and appenders at runtime.
// Access must be synchronized via the embedded mutex.
var global struct {
	mutex     sync.Mutex
	refreshed bool
	loggers   []Logger
	appenders []Appender
	named     map[string]Logger // configured loggers keyed by name (incl. root)
}

// RefreshConfig loads logging configuration from a flat map.
// It first expands inline expressions, then converts the result
// into a flatten.Storage and delegates to Refresh.
func RefreshConfig(m map[string]string) error {
	m, err := parseExpr(m)
	if err != nil {
		return err
	}
	p := flatten.NewProperties(m)
	return Refresh(flatten.NewPropertiesStorage(p))
}

// parseExpr expands inline map expressions embedded in values.
//
// A key ending with "!" indicates that its value is a map expression.
// The "!" suffix is stripped, and the parsed entries are flattened
// into "<key>.<subKey>" form.
//
// Example:
//
//	input:
//	  {
//	    "app.name": "MyApp",
//	    "db!": "DbConfig { host = localhost, port = 5432 }",
//	  }
//
//	output:
//	  {
//	    "app.name": "MyApp",
//	    "db.type":  "DbConfig",
//	    "db.host":  "localhost",
//	    "db.port":  "5432",
//	  }
//
// Returns an error if expression parsing fails or duplicate keys are detected.
func parseExpr(m map[string]string) (map[string]string, error) {
	ret := make(map[string]string)
	for k, v := range m {
		var ok bool
		k, ok = strings.CutSuffix(k, "!")
		if !ok {
			if _, exists := ret[k]; exists {
				return nil, errutil.Explain(nil, "duplicate key '%s'", k)
			}
			ret[k] = v
			continue
		}
		// Parse inline map expression
		subMap, err := expr.Parse(v)
		if err != nil {
			return nil, errutil.Explain(err, "parseExpr error")
		}
		for k2, v2 := range subMap {
			subKey := k + "." + k2
			if _, exists := ret[subKey]; exists {
				return nil, errutil.Explain(nil, "duplicate key '%s'", subKey)
			}
			ret[subKey] = v2
		}
	}
	return ret, nil
}

// Refresh rebuilds all loggers and appenders from the given configuration storage.
// It replaces the current runtime configuration atomically.
//
// The process includes:
//  1. Parsing logger and appender definitions
//  2. Instantiating plugins
//  3. Wiring appender references
//  4. Starting new components
//  5. Swapping in the new configuration
//  6. Stopping old components
//
// Returns an error if any step fails.
func Refresh(s flatten.Storage) error {

	global.mutex.Lock()
	defer global.mutex.Unlock()

	oldLoggers := global.loggers
	oldAppenders := global.appenders

	loggerNames := make(map[string]struct{})
	appenderNames := make(map[string]struct{})

	s.MapKeys("logger", loggerNames)
	s.MapKeys("appender", appenderNames)

	// Check logger definitions
	for _, l := range loggerMap {
		if _, ok := loggerNames[l.name]; !ok {
			return errutil.Explain(nil, "logger %s not found", l.name)
		}
	}

	// newPluginFromType creates a plugin instance from configuration.
	newPluginFromType := func(prefix string) (reflect.Value, string, error) {
		// The "type" attribute is read raw from storage, so unlike other
		// attributes it cannot use "${...}" property references.
		plugin, ok := s.Value(prefix + ".type")
		if !ok {
			return reflect.Value{}, "", errutil.Explain(nil, "attribute 'type' not found")
		}
		p, ok := pluginRegistry[plugin]
		if !ok {
			return reflect.Value{}, "", errutil.Explain(nil, "plugin %s not found", plugin)
		}
		v, err := newPlugin(p.Class, prefix, s)
		return v, plugin, err
	}

	var (
		cfgRoot      = defaultLogger
		cfgLoggers   = make(map[string]Logger)
		cfgAppenders = make(map[string]Appender)
		cfgTags      = make(map[string]Logger)
	)

	for name := range appenderNames {
		v, plugin, err := newPluginFromType("appender." + name)
		if err != nil {
			return errutil.Explain(err, "create appender %s error", name)
		}
		appender, ok := v.Interface().(Appender)
		if !ok {
			err = errutil.Explain(nil, "plugin %s does not implement log.Appender", plugin)
			return errutil.Explain(err, "create appender %s error", name)
		}
		cfgAppenders[name] = appender
	}

	// initAppenderRefs resolves and injects referenced appenders.
	initAppenderRefs := func(v reflect.Value) error {
		i, ok := v.Interface().(AppenderRefs)
		if !ok {
			return nil
		}
		syncMode, appenderRefs := i.GetAppenderRefs()
		for _, r := range appenderRefs {
			a, ok := cfgAppenders[r.Ref]
			if !ok {
				return errutil.Explain(nil, "appender %s not found", r.Ref)
			}
			// If sync mode is enabled, the appender must be concurrency-safe.
			if syncMode && !a.ConcurrentSafe() {
				return errutil.Explain(nil, "appender %s is not concurrent-safe", r.Ref)
			}
			r.Appender = a // assign resolved appender
		}
		return nil
	}

	cfgLoggers[RootLoggerName] = cfgRoot
	for name := range loggerNames {

		v, plugin, err := newPluginFromType("logger." + name)
		if err != nil {
			return errutil.Explain(err, "create logger %s error", name)
		}
		if err = initAppenderRefs(v); err != nil {
			return errutil.Explain(err, "init appender refs for logger %s error", name)
		}
		logger, ok := v.Interface().(Logger)
		if !ok {
			err = errutil.Explain(nil, "plugin %s does not implement log.Logger", plugin)
			return errutil.Explain(err, "create logger %s error", name)
		}
		cfgLoggers[name] = logger

		// Special handling for root logger
		if name == RootLoggerName {
			cfgRoot = logger
			continue
		}

		var tags []string
		for _, tag := range logger.GetTags() {
			if tag = strings.TrimSpace(tag); tag == "" {
				continue
			}
			// Only suffix wildcard patterns like "xxx_*" are allowed.
			if strings.Contains(tag, "*") {
				if !strings.HasSuffix(tag, "_*") {
					err = errutil.Explain(nil, "tag '%s' is invalid", tag)
					return errutil.Explain(err, "create logger %s error", name)
				}
			}
			tags = append(tags, tag)
		}
		if len(tags) == 0 {
			err = errutil.Explain(nil, "logger must have attribute 'tag'")
			return errutil.Explain(err, "create logger %s error", name)
		}

		// Register tag → logger mapping
		for _, strTag := range tags {
			if l, ok := cfgTags[strTag]; ok && l != logger {
				err = errutil.Explain(nil, "tag '%s' already config in logger %s", strTag, l)
				return errutil.Explain(err, "create logger %s error", name)
			}
			cfgTags[strTag] = logger
		}
	}

	var (
		success          bool
		startedLoggers   []Logger
		startedAppenders []Appender
	)

	defer func() {
		if !success {
			// Stop temp loggers and appenders
			for _, l := range startedLoggers {
				l.Stop()
			}
			for _, a := range startedAppenders {
				a.Stop()
			}
		}
	}()

	// Start new appenders and loggers
	for _, a := range cfgAppenders {
		if err := a.Start(); err != nil {
			return errutil.Explain(err, "appender %s start error", a.GetName())
		}
		startedAppenders = append(startedAppenders, a)
	}
	for _, l := range cfgLoggers {
		if err := l.Start(); err != nil {
			return errutil.Explain(err, "logger %s start error", l.GetName())
		}
		startedLoggers = append(startedLoggers, l)
	}
	success = true

	// Bind named loggers
	for _, l := range loggerMap {
		l.logger.Store(&loggerValue{cfgLoggers[l.name]})
	}

	// findLogger selects the most specific logger for a given tag,
	// falling back hierarchically using "_*" patterns.
	findLogger := func(tag string) Logger {
		for {
			if l, ok := cfgTags[tag]; ok {
				return l
			}
			tag, _ = strings.CutSuffix(tag, "_*")
			i := strings.LastIndex(tag, "_")
			if i <= 0 {
				return cfgRoot
			}
			tag = tag[:i] + "_*"
		}
	}

	// Bind tag-based loggers
	for tag, l := range tagRegistry {
		l.logger.Store(&loggerValue{findLogger(tag)})
	}

	global.loggers = slices.Collect(maps.Values(cfgLoggers))
	global.appenders = slices.Collect(maps.Values(cfgAppenders))
	global.named = cfgLoggers
	global.refreshed = true

	// Stop old loggers and appenders
	for _, l := range oldLoggers {
		l.Stop()
	}
	for _, a := range oldAppenders {
		a.Stop()
	}

	return nil
}

// Destroy gracefully shuts down all loggers and appenders,
// releases resources, and resets global state.
func Destroy() {
	global.mutex.Lock()
	defer global.mutex.Unlock()

	for _, obj := range tagRegistry {
		obj.reset()
	}
	for _, obj := range loggerMap {
		obj.reset()
	}

	// Stop all loggers and appenders
	for _, l := range global.loggers {
		l.Stop()
	}
	for _, a := range global.appenders {
		a.Stop()
	}
	global.loggers = nil
	global.appenders = nil
	global.named = nil
	global.refreshed = false
}
