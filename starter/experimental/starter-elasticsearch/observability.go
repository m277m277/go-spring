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

package StarterElasticsearch

import (
	"net/http"

	"github.com/elastic/elastic-transport-go/v8/elastictransport"
	observe "go-spring.org/observe"
)

// newOtelInstrumentation builds the transport-level OpenTelemetry
// instrumentation plugged into elasticsearch.Config.Instrumentation. Passing a
// nil TracerProvider makes the transport emit client spans through the OTel
// global TracerProvider that starter-otel installs; when starter-otel is absent
// that global is a no-op, so this is a zero-config opt-in that needs no
// per-component adaptation.
func newOtelInstrumentation() *elastictransport.ElasticsearchOpenTelemetry {
	return elastictransport.NewOtelInstrumentation(nil, false, "")
}

// newObserveTransport wraps the underlying HTTP round-tripper so each request
// emits a duration metric + access log via the observe kit. It is built with
// WithoutTrace: the trace span comes from newOtelInstrumentation above (applied
// at the elastictransport.Perform layer, above this round-tripper), so the kit
// only fills the metric+log gap - no duplicate span. The operation is derived
// from the request method + URL path (e.g. "POST /index/_search").
func newObserveTransport(cfg observe.LogConfig) http.RoundTripper {
	return &obsTransport{
		base: http.DefaultTransport,
		obs:  observe.NewClient("elasticsearch", cfg, observe.WithoutTrace()),
	}
}

type obsTransport struct {
	base http.RoundTripper
	obs  *observe.Observer
}

func (t *obsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	op := req.Method + " " + req.URL.Path
	ctx, sp := t.obs.Start(req.Context(), op, req.URL.Path)
	resp, err := t.base.RoundTrip(req.WithContext(ctx))
	sp.End(err)
	return resp, err
}
