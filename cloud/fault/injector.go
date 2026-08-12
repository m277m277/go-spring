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

package fault

import (
	"math/rand/v2"
	"sync/atomic"
	"time"
)

// Injector holds the live [Config] behind an atomic pointer so a starter can
// hot-swap it from a gs.Dync.OnChanged callback without taking a lock on the
// execute path. A zero-valued config (or Enabled false) injects nothing, so an
// Injector with the zero Config is a transparent no-op.
type Injector struct {
	cfg atomic.Pointer[Config]
}

// NewInjector returns an Injector holding c. The config can be replaced later
// with [Injector.SetConfig].
func NewInjector(c Config) *Injector {
	in := &Injector{}
	in.cfg.Store(&c)
	return in
}

// SetConfig atomically swaps the live config. It is safe to call concurrently
// with Execute; each call observes the config pointer current at its own
// [Injector.maybe] read, so a toggle takes effect on the next operation.
func (in *Injector) SetConfig(c Config) {
	in.cfg.Store(&c)
}

// Config returns a snapshot of the live config (the zero Config if none has
// been set, which is a no-op).
func (in *Injector) Config() Config {
	if c := in.cfg.Load(); c != nil {
		return *c
	}
	return Config{}
}

// maybe decides the fault applied to one call to resource:
//   - sleep is a latency to apply first (always, when Latency > 0);
//   - inject reports whether an error should be returned instead of calling the
//     real operation fn, decided at the configured Rate;
//   - err is the error to return when inject is true.
//
// resource is accepted for the per-resource rules extension; the global MVP
// rule applies uniformly regardless of resource.
func (in *Injector) maybe(resource string) (inject bool, sleep time.Duration, err error) {
	_ = resource // reserved for per-resource rules
	c := in.Config()
	if !c.Enabled {
		return false, 0, nil
	}
	if c.Latency > 0 {
		sleep = c.Latency
	}
	if c.Rate > 0 && rand.Float64() < c.Rate {
		inject = true
		err = mapError(c.Error)
	}
	return inject, sleep, err
}
