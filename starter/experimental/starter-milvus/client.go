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

// client.go is the resource entity + lifecycle of this starter. Milvus's SDK
// exposes no reject-capable interceptor seam (gRPC dial options are build-time
// only, not a per-op guard), so there is no per-operation resilience — the
// wrapper is a thin holder with a fail-fast probe and health check, matching
// the cassandra iterator-path stance (see DESIGN).
package StarterMilvus

import (
	"context"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
)

// Client is the bean Milvus connections are injected as. It embeds the SDK's
// client.Client interface (so every method promotes unchanged) and holds the
// config for the health probe.
type Client struct {
	client.Client
	cfg Config
}

// newClient builds the Milvus client and probes it once so a wrong address or
// bad credential fails fast at startup instead of on first query.
func newClient(ctx context.Context, c Config) (*Client, error) {
	cl, err := client.NewClient(ctx, client.Config{
		Address:  c.Addr,
		Username: c.Username,
		Password: c.Password,
		DBName:   c.Database,
	})
	if err != nil {
		return nil, err
	}
	// Fail-fast probe: listing collections verifies reachability + auth.
	if _, err := cl.ListCollections(ctx); err != nil {
		_ = cl.Close()
		return nil, err
	}
	return &Client{Client: cl, cfg: c}, nil
}

// Destroy closes the connection.
func (o *Client) Destroy() error {
	return o.Client.Close()
}

// Health reports whether Milvus answers a trivial query (list collections).
func (o *Client) Health(ctx context.Context) error {
	_, err := o.Client.ListCollections(ctx)
	return err
}
