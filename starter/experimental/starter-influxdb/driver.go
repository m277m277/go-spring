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

// driver.go is the "construction seam" concept of this starter: the Driver
// interface + registry + DefaultDriver, which owns full client assembly (the
// injected HTTP client carrying the dynamic transport that Init later arms).
// It mirrors starter-elasticsearch's driver.go.
package StarterInfluxdb

import (
	"context"
	"net/http"
	"sync"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
)

var driverRegistry = map[string]Driver{}

func init() {
	RegisterDriver("DefaultDriver", DefaultDriver{})
}

// Driver interface defines how to create an InfluxDB client (an
// influxdb2.Client). It is the single extension point for customizing client
// assembly: an app (or the bundled DefaultDriver) implements it once and
// registers via RegisterDriver; callers select one through Config.Driver,
// which defaults to "DefaultDriver".
type Driver interface {
	CreateClient(ctx context.Context, c Config) (influxdb2.Client, error)
}

// RegisterDriver registers an InfluxDB driver with the given name.
// It panics if the driver name has already been registered.
func RegisterDriver(name string, driver Driver) {
	if _, ok := driverRegistry[name]; ok {
		panic("influxdb driver already registered: " + name)
	}
	driverRegistry[name] = driver
}

// DefaultDriver is the default implementation of the Driver interface.
type DefaultDriver struct{}

// CreateClient creates a new influxdb2.Client from the provided
// configuration.
//
// The HTTP client is injected through Options so requests ride the
// dynamicTransport — an atomic RoundTripper indirection whose behavior Init
// later swaps in (the observe+resilience transport built from the injected
// policy). The dynamic transport is tracked in [dynamicTransports] (keyed by
// the returned client) so newClient can hand it to the wrapper.
func (DefaultDriver) CreateClient(ctx context.Context, c Config) (influxdb2.Client, error) {
	dyn := newDynamicTransport()
	opts := influxdb2.DefaultOptions().SetHTTPClient(&http.Client{Transport: dyn})
	cl := influxdb2.NewClientWithOptions(c.ServerURL, c.AuthToken, opts)
	dynamicTransports.Store(cl, dyn)
	return cl, nil
}

// dynamicTransports tracks the dynamic transport DefaultDriver installed for
// each client, so newClient can hand it to the wrapper for Init to arm. The
// key is the influxdb2.Client value; only clients built by DefaultDriver
// appear here.
var dynamicTransports sync.Map // influxdb2.Client -> *dynamicTransport
