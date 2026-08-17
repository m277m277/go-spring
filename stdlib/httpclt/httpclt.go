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

// Package httpclt is the runtime the generated declarative HTTP client calls
// into. It holds no state: it carries request [Metadata], applies
// [RequestOption]s, encodes the body via stdlib/jsonflow, and dispatches the
// call through [DoRequest] — a replaceable var whose default is
// http.DefaultClient. It knows nothing about discovery, load balancing,
// resilience, or tracing — those belong in a replaced [DoRequest].
package httpclt

import (
	"bytes"
	"context"
	"io"
	"maps"
	"net/http"

	"go-spring.org/stdlib/jsonflow"
)

// QueryStringer defines the method to convert an object to a query string
// format (e.g., "key1=value1&key2=value2").
type QueryStringer interface {
	QueryForm() (string, error)
}

// Metadata holds contextual information for an HTTP request.
type Metadata struct {
	Target string // Service name or IP:PORT
	Schema string // HTTP protocol (e.g., http, https)
	Method string // HTTP method (GET, POST, etc.)
	// Pattern is the request path with placeholders (for REST). It is part of
	// the gs-http-gen output contract, so it must stay even though the runtime
	// only reads RawPath (the path with placeholders already processed).
	Pattern string
	RawPath string            // Request path with placeholders processed
	Query   QueryStringer     // Query string after the '?' part
	Body    any               // Request body
	Header  http.Header       // Request headers
	Config  map[string]string // Additional configuration options
}

// RequestOption is a function that modifies the Metadata.
type RequestOption func(meta *Metadata)

// CombineMetadata applies the given RequestOptions to the Metadata.
func CombineMetadata(meta Metadata, opts ...RequestOption) Metadata {
	for _, opt := range opts {
		opt(&meta)
	}
	return meta
}

// WithHeader is a RequestOption that adds the given HTTP headers to the Metadata.
func WithHeader(header http.Header) RequestOption {
	return func(meta *Metadata) {
		if meta.Header == nil {
			meta.Header = http.Header{}
		}
		maps.Copy(meta.Header, header)
	}
}

// WithConfig is a RequestOption that adds the given configuration map to the Metadata.
func WithConfig(config map[string]string) RequestOption {
	return func(meta *Metadata) {
		if meta.Config == nil {
			meta.Config = map[string]string{}
		}
		maps.Copy(meta.Config, config)
	}
}

// DoRequest performs the actual HTTP request and decodes the response. The
// default sends through http.DefaultClient; a starter or the application may
// replace this var wholesale to add logging, metrics, tracing, or a custom
// transport — it is the single dispatch extension point. The replacement
// receives the prepared *http.Request plus [Metadata] and the decode callback,
// so it controls the whole request/response lifecycle.
var DoRequest = func(req *http.Request, meta Metadata, fn func(io.Reader) error) (*http.Response, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if err = fn(resp.Body); err != nil {
		return nil, err
	}
	return resp, nil
}

// doRequest executes the HTTP request, preparing the body and metadata before sending the request.
func doRequest(ctx context.Context, meta Metadata, fn func(io.Reader) error) (*http.Response, error) {

	if meta.Query != nil {
		if s, err := meta.Query.QueryForm(); err != nil {
			return nil, err
		} else if s != "" {
			meta.RawPath += "?" + s
		}
	}

	buf := bytes.NewBuffer(nil)
	if v, ok := meta.Body.(interface{ EncodeForm() (string, error) }); ok {
		if s, err := v.EncodeForm(); err != nil {
			return nil, err
		} else if s != "" {
			buf.WriteString(s)
		}
	} else if meta.Body != nil {
		if err := jsonflow.MarshalWrite(buf, meta.Body); err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, meta.Method, meta.RawPath, buf)
	if err != nil {
		return nil, err
	}

	req.Host = meta.Target
	req.URL.Host = meta.Target
	req.URL.Scheme = meta.Schema
	maps.Copy(req.Header, meta.Header)

	return DoRequest(req, meta, fn)
}

// ResponseObject is an interface that can decode the response body from JSON using streaming.
type ResponseObject interface {
	DecodeJSON(d jsonflow.Decoder) error
}

// ObjectResponse decodes the response body into a given object using streaming JSON parsing.
func ObjectResponse[T ResponseObject](ctx context.Context, o T, meta Metadata) (*http.Response, T, error) {
	resp, err := doRequest(ctx, meta, func(r io.Reader) error {
		return o.DecodeJSON(jsonflow.NewDecoder(r))
	})
	if err != nil {
		return nil, o, err
	}
	return resp, o, nil
}

// JSONResponse decodes the response body into a generic JSON object.
func JSONResponse[T any](ctx context.Context, meta Metadata) (_ *http.Response, o T, _ error) {
	resp, err := doRequest(ctx, meta, func(r io.Reader) error {
		return jsonflow.UnmarshalRead(r, &o)
	})
	if err != nil {
		return nil, o, err
	}
	return resp, o, nil
}
