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

package httpclt_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"go-spring.org/stdlib/hashutil"
	"go-spring.org/stdlib/httpclt"
	"go-spring.org/stdlib/jsonflow"
	"go-spring.org/stdlib/testing/assert"
)

type HelloRequest struct {
	HelloRequestBody
	Message string `json:"message" query:"message" validate:"required"`
}

func (x *HelloRequest) QueryForm() (string, error) {
	m := make(url.Values)
	m.Add("message", x.Message)
	return m.Encode(), nil
}

type HelloRequestBody struct{}

type HelloResponse struct {
	Message *string `json:"message,omitempty" form:"message"`
}

func NewHelloResponse() *HelloResponse {
	return &HelloResponse{}
}

func (r *HelloResponse) DecodeJSON(d jsonflow.Decoder) (err error) {
	const (
		hashMessage = 0x546401b5d2a8d2a4 // HashKey("message")
	)

	if err = jsonflow.DecodeObjectBegin(d); err != nil {
		return err
	}

	for d.PeekKind() != '}' {

		var key string
		key, err = jsonflow.DecodeString(d)
		if err != nil {
			return err
		}

		switch hashutil.FNV1a64(key) {
		case hashMessage:
			if r.Message, err = jsonflow.DecodeStringPtr(d); err != nil {
				return err
			}
		default:
			if err = d.SkipValue(); err != nil {
				return err
			}
		}
	}

	if err = jsonflow.DecodeObjectEnd(d); err != nil {
		return err
	}
	return
}

// formBody implements EncodeForm so httpclt sends it form-encoded.
type formBody struct {
	Values url.Values
}

func (f *formBody) EncodeForm() (string, error) {
	return f.Values.Encode(), nil
}

func TestObjectResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fmt.Appendf(nil, `{"message": "hello %s"}`, r.URL.Query().Get("message")))
	}))
	defer server.Close()

	meta := httpclt.Metadata{
		Target:  server.Listener.Addr().String(),
		Schema:  "http",
		Method:  http.MethodGet,
		Pattern: "/v1/hello",
		RawPath: "/v1/hello",
		Query:   &HelloRequest{Message: "world"},
		Header: http.Header{
			"Content-Type": []string{"application/x-www-form-urlencoded"},
			"Accept":       []string{"application/json"},
		},
	}

	_, resp, err := httpclt.ObjectResponse(context.Background(), NewHelloResponse(), meta)
	assert.Error(t, err).Nil()
	assert.That(t, resp).Equal(&HelloResponse{Message: new("hello world")})
}

func TestJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.That(t, r.Header.Get("X-Request-ID")).Equal("12345678")
		_, _ = w.Write([]byte(`{"message":"hello json"}`))
	}))
	defer server.Close()

	meta := httpclt.Metadata{
		Target:  server.Listener.Addr().String(),
		Schema:  "http",
		Method:  http.MethodGet,
		RawPath: "/v1/hello",
	}

	h := http.Header{}
	h.Set("X-Request-ID", "12345678")

	_, out, err := httpclt.JSONResponse[*HelloResponse](context.Background(),
		httpclt.CombineMetadata(meta, httpclt.WithHeader(h)))
	assert.Error(t, err).Nil()
	assert.That(t, out.Message).Equal(new("hello json"))
}

func TestJSONResponseError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":`)) // invalid JSON
	}))
	defer server.Close()

	meta := httpclt.Metadata{
		Target:  server.Listener.Addr().String(),
		Schema:  "http",
		Method:  http.MethodGet,
		RawPath: "/v1/hello",
	}

	_, _, err := httpclt.JSONResponse[*HelloResponse](context.Background(), meta)
	assert.Error(t, err).NotNil()
}

func TestWithConfigAndCombineMetadata(t *testing.T) {
	meta := httpclt.Metadata{
		Target:  "user-svc",
		Schema:  "http",
		Method:  http.MethodGet,
		RawPath: "/v1/hello",
		Config:  map[string]string{"existing": "1"},
	}

	got := httpclt.CombineMetadata(meta,
		httpclt.WithConfig(map[string]string{"timeout": "3s"}),
		httpclt.WithHeader(http.Header{"X-Trace": []string{"1"}}),
	)

	// WithConfig merges into the existing map instead of replacing it.
	assert.That(t, got.Config).Equal(map[string]string{"existing": "1", "timeout": "3s"})
	assert.That(t, got.Header.Get("X-Trace")).Equal("1")

	// CombineMetadata copies the Metadata struct: the base had no Header, so
	// WithHeader's new map lives only on the copy. (Config maps are reference
	// types, so the merge is visible through both — don't share base maps when
	// that matters.)
	assert.That(t, meta.Header).Nil()
}

func TestEncodeFormBody(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	meta := httpclt.Metadata{
		Target:  server.Listener.Addr().String(),
		Schema:  "http",
		Method:  http.MethodPost,
		RawPath: "/v1/hello",
		Header: http.Header{
			"Content-Type": []string{"application/x-www-form-urlencoded"},
		},
		Body: &formBody{Values: url.Values{"message": []string{"world"}}},
	}

	_, _, err := httpclt.JSONResponse[map[string]any](context.Background(), meta)
	assert.Error(t, err).Nil()
	assert.That(t, gotBody).Equal("message=world")
}
