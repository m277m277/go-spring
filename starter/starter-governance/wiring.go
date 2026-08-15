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

package StarterGovernance

import (
	"go-spring.org/cloud/governance"
	"go-spring.org/spring/gs"
)

// This file is the DEFAULT WIRING between gs and the container-free governance
// core (cloud/governance). Blank-importing starter-governance registers one
// root bean that binds ${govern} properties into the governance center and arms
// it — the whole plain-${govern} experience, with every conf provider's watch
// (file fsnotify, nacos ListenConfig, k8s informer, consul, vault, bus) working
// through gs's two-phase refresh, unchanged. The conditional source adapters in
// this module (file/http) and in the config starters (nacos/etcd) replace the
// default through the same governance.Source contract.

// wiring is the always-registered bean that connects gs to the governance
// singleton. It holds the ${govern} gs.Dync (the default source's data feed)
// and the optional bean-injected custom source.
type wiring struct {
	// Gov is the DEFAULT governance source: the ${govern} binding, field-
	// injected and hot-reloaded, adapted to governance.Source by dyncSource.
	// It is a gs.Dync field unconditionally — an app with no Dync field
	// anywhere would drop its whole properties-refresh path (gs keeps config
	// alive only while some Dync field holds it), even when a custom source
	// drives governance.
	Gov gs.Dync[governance.Config] `value:"${govern:=}"`

	// Src is the bean-friendly custom source: gs field-injects here any bean
	// exported as a governance.Source (autowire:"?" is nullable — no such bean
	// leaves it nil and the default ${govern} path runs). Source priority is
	// an explicit governance.SetSource (any time) > Src > the ${govern} Dync.
	Src governance.Source `autowire:"?"`
}

func init() {
	// Exported as a gs.Rooter so gs collects and instantiates the wiring even
	// though no client injects it — without a collected-type export gs would
	// not instantiate an unreachable bean, so none of the registrations fire.
	gs.Provide(newWiring).
		Init((*wiring).Init).Destroy((*wiring).Destroy).
		Export(gs.As[gs.Rooter]()).Caller(1)
}

func newWiring() *wiring { return &wiring{} }

// Init binds the default source (a bean-injected Source when present, else the
// ${govern} Dync adapter) and completes the authority's startup. An explicit
// governance.SetSource called before wiring wins — BindDefault is a no-op when
// a source is already bound.
func (w *wiring) Init() error {
	if w.Src != nil {
		governance.BindDefault(w.Src)
	} else {
		governance.BindDefault(dyncSource{gov: &w.Gov})
	}
	governance.GoLive()
	return nil
}

// Destroy closes the active source when it happens to be closeable (the
// Source contract keeps Close optional), via the governance facade.
func (w *wiring) Destroy() error { return governance.CloseActiveSource() }

// dyncSource adapts the field-injected ${govern} gs.Dync to the
// governance.Source contract. When no custom source is registered, this is
// what drives the center, so the plain-${govern} path behaves exactly as it
// always has.
type dyncSource struct{ gov *gs.Dync[governance.Config] }

func (s dyncSource) Snapshot() governance.Config { return s.gov.Value() }

func (s dyncSource) Subscribe(cb func(governance.Config)) {
	s.gov.OnChanged(func(new, _ governance.Config) { cb(new) })
}
