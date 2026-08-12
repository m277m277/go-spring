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

package StarterNats

import (
	"time"

	"go-spring.org/cloud/fault"
	"go-spring.org/cloud/resilience"
	"go-spring.org/cloud/tlsconf"
	observe "go-spring.org/observe"
)

// Config defines NATS client connection configuration.
type Config struct {
	// URL is the NATS server URL, e.g., "nats://127.0.0.1:4222".
	// Multiple servers may be comma-separated.
	URL string `value:"${url}" expr:"$ != ''"`

	// Name is the connection name reported to the server, default is empty.
	Name string `value:"${name:=}"`

	// Username is the auth username, default is empty.
	Username string `value:"${username:=}"`

	// Password is the auth password, default is empty.
	Password string `value:"${password:=}"`

	// Token is the auth token, an alternative to username/password,
	// default is empty.
	Token string `value:"${token:=}"`

	// CredsFile is the path to a NATS credentials file (JWT + nkey seed),
	// used for decentralized (NATS 2.x / NGS) auth. Default is empty.
	CredsFile string `value:"${creds-file:=}"`

	// NKeyFile is the path to an nkey seed file used for nkey auth,
	// an alternative to CredsFile. Default is empty.
	NKeyFile string `value:"${nkey-file:=}"`

	// TLS configures the transport security for the connection.
	TLS tlsconf.TLSConfig `value:"${tls}"`

	// MaxReconnects is the maximum number of reconnect attempts,
	// -1 means unlimited, default is 60.
	MaxReconnects int `value:"${max-reconnects:=60}"`

	// ReconnectWait is the delay between reconnect attempts, default is "2s".
	ReconnectWait time.Duration `value:"${reconnect-wait:=2s}"`

	// ConnectTimeout bounds how long the initial dial waits, default is "5s".
	ConnectTimeout time.Duration `value:"${connect-timeout:=5s}"`

	// JetStream configures the JetStream context derived from this connection.
	JetStream JetStreamConfig `value:"${jetstream}"`

	// Resilience optionally protects outbound calls with rate limiting and
	// circuit breaking. It is disabled by default; when enabled the opt-in
	// PublishGuarded/RequestGuarded methods on Conn route the outbound call
	// through the selected resilience driver. nats has no reject-capable
	// middleware seam, so plain Publish/Request stay unchanged — callers pick
	// per-call whether they want the guard.
	Resilience resilience.Config `value:"${resilience:=}"`

	// Fault optionally injects failures/latency into outbound calls so retry/
	// breaker can be proven under load. Disabled by default.
	Fault fault.Config `value:"${fault:=}"`

	// Observability configures the per-operation instrumentation (producer span +
	// duration/in-flight metric + access log off/brief/detailed) emitted by the
	// Conn.PublishMsg wrapper and the binder's consume callback. Defaults to
	// "brief".
	Observability observe.ObserveConfig `value:"${observability:=}"`
}

// Resilience binds the backend-neutral resilience knobs shared by every client
// starter (see [resilience.Config]). Driver selects which registered backend
// enforces them: "default" (bundled, zero-dependency) or "sentinel"
// (recommended, enabled by blank-importing starter-resilience). Switching
// backends is a one-line config change — no code touches the guard seam.
//
// Keep MaxRetries at 0 for publishing — retrying a publish can duplicate a
// message; leave retry to the caller who knows whether the message is idempotent.

// JetStream context is created from the connection and exposed on Conn.JetStream;
// otherwise Conn.JetStream is nil.
type JetStreamConfig struct {
	// Enabled turns on the JetStream context for this connection,
	// default is false.
	Enabled bool `value:"${enabled:=false}"`
}
