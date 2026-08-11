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

package StarterGormMySql

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/go-sql-driver/mysql"
	"go-spring.org/cloud/discovery"
	"go-spring.org/cloud/mesh"
)

// liveDialers tracks the discovery-backed dialer and its registered network
// name behind each client, so the wrapper's Close can stop the watch and
// deregister.
var liveDialers sync.Map // *gorm.DB -> *discoveryConn

// netSeq makes each registered mysql dial network name unique, so multiple
// instances discovering the same service never collide.
var netSeq atomic.Uint64

// discoveryConn pairs a live Resolver with the unique mysql dial network name
// it registered, so the Close-half can stop the watch and deregister the dialer.
type discoveryConn struct {
	ld      *discovery.Resolver
	netName string
}

// newDiscoveryConn resolves the registered discovery backend for c and registers
// a mysql dialer that routes each new connection through a live endpoint. It
// returns (nil, nil) when service-name is unset or mesh mode is enabled (a
// sidecar owns discovery+LB), in which case the caller dials the configured Addr
// directly. The caller owns the lifecycle and must release the conn via
// stopDiscoveryConn.
func newDiscoveryConn(ctx context.Context, c Config) (*discoveryConn, error) {
	if c.ServiceName == "" || mesh.Enabled() {
		return nil, nil
	}
	d, err := discovery.GetDiscovery(c.Discovery)
	if err != nil {
		return nil, err
	}
	ld, err := discovery.NewResolver(ctx, d, c.ServiceName, discovery.WithScheme(c.Scheme))
	if err != nil {
		return nil, err
	}
	netName := fmt.Sprintf("gsdisco_%s_%d", c.ServiceName, netSeq.Add(1))
	nd := &net.Dialer{}
	// mysql.DialContextFunc is 2-arg: func(ctx, addr string) (net.Conn, error).
	// The addr is ignored; the dialer picks a live endpoint via the Resolver.
	mysql.RegisterDialContext(netName, func(ctx context.Context, _ string) (net.Conn, error) {
		ep, perr := ld.Pick()
		if perr != nil {
			return nil, perr
		}
		return nd.DialContext(ctx, "tcp", ep.Addr)
	})
	return &discoveryConn{ld: ld, netName: netName}, nil
}

// stopDiscoveryConn stops the discovery watch and deregisters the mysql dialer
// behind conn. It is the Close-half of the discovery lifecycle, symmetric with
// newDiscoveryConn; it is a no-op for a nil conn (a client that never had one).
func stopDiscoveryConn(conn *discoveryConn) {
	if conn == nil {
		return
	}
	_ = conn.ld.Stop()
	mysql.DeregisterDialContext(conn.netName)
}
