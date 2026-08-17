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
// interface + registry + DefaultDriver, which owns full session assembly
// (ClusterConfig, authenticator, consistency, TLS, timeouts). It mirrors
// starter-s3's driver.go.
package StarterCassandra

import (
	"context"
	"crypto/tls"

	"github.com/gocql/gocql"
	"go-spring.org/stdlib/errutil"
)

var driverRegistry = map[string]Driver{}

func init() {
	RegisterDriver("DefaultDriver", DefaultDriver{})
}

// Driver interface defines how to create a Cassandra session (a *gocql.
// Session). It is the single extension point for customizing session
// assembly: an app (or the bundled DefaultDriver) implements it once and
// registers via RegisterDriver; callers select one through Config.Driver,
// which defaults to "DefaultDriver".
type Driver interface {
	CreateClient(ctx context.Context, c Config) (*gocql.Session, error)
}

// RegisterDriver registers a Cassandra driver with the given name.
// It panics if the driver name has already been registered.
func RegisterDriver(name string, driver Driver) {
	if _, ok := driverRegistry[name]; ok {
		panic("cassandra driver already registered: " + name)
	}
	driverRegistry[name] = driver
}

// DefaultDriver is the default implementation of the Driver interface.
type DefaultDriver struct{}

// CreateClient creates a new gocql.Session from the provided configuration.
// It owns full session assembly — hosts, PasswordAuthenticator, consistency
// level, timeouts, TLS — but not the startup probe or the resilience wiring,
// which are the starter's lifecycle concerns (see newClient in starter.go).
func (DefaultDriver) CreateClient(ctx context.Context, c Config) (*gocql.Session, error) {
	consistency, err := parseConsistency(c.Consistency)
	if err != nil {
		return nil, err
	}
	cfg := gocql.NewCluster(c.Hosts...)
	cfg.Keyspace = c.Keyspace
	cfg.Consistency = consistency
	cfg.Timeout = c.Timeout
	cfg.ConnectTimeout = c.ConnectTimeout
	cfg.CQLVersion = c.CQLVersion
	if c.Username != "" {
		cfg.Authenticator = gocql.PasswordAuthenticator{Username: c.Username, Password: c.Password}
	}
	if c.TLS.Enabled {
		cfg.SslOpts = &gocql.SslOptions{
			Config: &tls.Config{
				ServerName:         c.TLS.ServerName,
				InsecureSkipVerify: c.TLS.InsecureSkipVerify,
			},
			CertPath:               c.TLS.CertFile,
			KeyPath:                c.TLS.KeyFile,
			CaPath:                 c.TLS.CAFile,
			EnableHostVerification: !c.TLS.InsecureSkipVerify,
		}
	}
	session, err := cfg.CreateSession()
	if err != nil {
		return nil, errutil.Explain(err, "cassandra: create session failed for %v", c.Hosts)
	}
	return session, nil
}

// parseConsistency maps the config string onto gocql.Consistency.
func parseConsistency(s string) (gocql.Consistency, error) {
	switch s {
	case "", "local-quorum":
		return gocql.LocalQuorum, nil
	case "any":
		return gocql.Any, nil
	case "one":
		return gocql.One, nil
	case "two":
		return gocql.Two, nil
	case "three":
		return gocql.Three, nil
	case "quorum":
		return gocql.Quorum, nil
	case "all":
		return gocql.All, nil
	case "each-quorum":
		return gocql.EachQuorum, nil
	case "local-one":
		return gocql.LocalOne, nil
	default:
		return 0, errutil.Explain(nil, "cassandra: unknown consistency %q (want any|one|two|three|quorum|all|local-quorum|each-quorum|local-one)", s)
	}
}
