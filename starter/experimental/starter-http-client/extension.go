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

package StarterHTTPClient

import (
	"net/http"
	"sync"
)

// BaseTransportFactory builds the base (lowest) http.RoundTripper for a named
// client instance — the layer that ultimately dials the wire, below discovery,
// load balancing, resilience and traffic. It replaces the default
// otelhttp-instrumented http.DefaultTransport, so an application that needs a
// custom *http.Transport (proxy, connection pool tuning, a custom dialer) can
// supply one without forking the starter. Returning nil keeps the default for
// that instance, so a factory can opt in per name. Install one via
// [SetBaseTransportFactory]; it is consulted for every instance at wiring time.
//
// This is the "factory/driver" extension point for the http client: when the
// assembled object is what the user wants to control, expose how the base is
// built rather than the whole fixed chain.
type BaseTransportFactory func(name string, c Config) http.RoundTripper

// TransportMiddleware wraps the fully-assembled transport
// (otel base → discovery/load-balance → resilience → traffic) for a named
// client instance. It is the "middleware" extension point — the place to add an
// auth header, a custom metric, request/response filtering, or any cross-cutting
// concern without replacing or disabling the built-in stack. Registered
// middleware compose in registration order, first registered = outermost.
type TransportMiddleware func(name string, c Config, next http.RoundTripper) http.RoundTripper

// Extension state is written during process init / before the container wires
// (the same window RegisterDriver-style registries use) and read once per
// instance when the container calls newClient. A mutex guards it so registration
// is safe regardless of timing — registration is rare, so the lock is free in
// practice.
var (
	extMu             sync.RWMutex
	baseFactory       BaseTransportFactory
	transportWrappers []TransportMiddleware
)

// SetBaseTransportFactory installs f as the base-transport factory consulted
// when newClient builds each instance's transport. Call from an init function
// (or otherwise before the container wires); the last call wins. Pass nil to
// revert to the default otelhttp base.
func SetBaseTransportFactory(f BaseTransportFactory) {
	extMu.Lock()
	defer extMu.Unlock()
	baseFactory = f
}

// UseTransportMiddleware appends m to the per-instance transport chain.
// Middleware run in registration order, outermost first. Call from an init
// function (or otherwise before the container wires). Multiple calls compose
// several concerns into one chain.
func UseTransportMiddleware(m TransportMiddleware) {
	extMu.Lock()
	defer extMu.Unlock()
	transportWrappers = append(transportWrappers, m)
}

// currentBaseFactory returns the installed base factory (nil when none).
func currentBaseFactory() BaseTransportFactory {
	extMu.RLock()
	defer extMu.RUnlock()
	return baseFactory
}

// currentTransportWrappers returns a snapshot copy of the registered middleware
// so later registrations cannot mutate the slice a wiring is iterating.
func currentTransportWrappers() []TransportMiddleware {
	extMu.RLock()
	defer extMu.RUnlock()
	return append([]TransportMiddleware(nil), transportWrappers...)
}
