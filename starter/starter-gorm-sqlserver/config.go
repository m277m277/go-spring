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

package StarterGormSqlserver

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"go-spring.org/cloud/tlsconf"
	gormcore "go-spring.org/starter-gorm"
)

// Config holds the configuration parameters for a SQL Server connection. The
// shared pool/discovery/observe settings come from the embedded gormcore.Common;
// the fields below are the SQL Server-specific connection parameters.
type Config struct {
	gormcore.Common

	User     string `value:"${user}"`       // Database username
	Password string `value:"${password}"`   // Database password
	Host     string `value:"${host:=}"`     // Database host (required unless ServiceName is set)
	Port     string `value:"${port:=1433}"` // Database port
	DB       string `value:"${db}"`         // Database name

	// Connect/dial timeouts. SQL Server has no DSN-level read/write timeout;
	// per-operation deadlines are driven through context. A zero value leaves
	// the driver default in place.
	DialTimeout    time.Duration `value:"${dialTimeout:=}"`    // TCP dial timeout
	ConnectTimeout time.Duration `value:"${connectTimeout:=}"` // Login/connection timeout

	// TLS uses the shared tlsconf.TLSConfig block (nested keys:
	// tls.enabled, tls.insecure-skip-verify, tls.ca-file). SQL Server maps
	// them onto DSN parameters rather than a *tls.Config: TLS.Enabled →
	// "encrypt=true"; TLS.InsecureSkipVerify → "TrustServerCertificate=true";
	// TLS.CAFile → "certificate" (a PEM server certificate / CA path). The
	// CertFile/KeyFile fields are unused because the DSN has no client-cert slot.
	TLS tlsconf.TLSConfig `value:"${tls}"`
}

// DSN constructs the SQL Server Data Source Name based on the configuration.
// Format: sqlserver://<user>:<password>@<host>:<port>?database=<db>&...
func (c Config) DSN() string {
	var sb strings.Builder
	sb.WriteString("sqlserver://")
	sb.WriteString(url.QueryEscape(c.User))
	sb.WriteString(":")
	sb.WriteString(url.QueryEscape(c.Password))
	sb.WriteString("@")
	sb.WriteString(c.Host)
	sb.WriteString(":")
	sb.WriteString(c.Port)
	sb.WriteString("?database=")
	sb.WriteString(url.QueryEscape(c.DB))

	if c.DialTimeout != 0 {
		sb.WriteString("&dial+timeout=")
		sb.WriteString(strconv.Itoa(int(c.DialTimeout.Seconds())))
	}
	if c.ConnectTimeout != 0 {
		sb.WriteString("&connection+timeout=")
		sb.WriteString(strconv.Itoa(int(c.ConnectTimeout.Seconds())))
	}
	if c.TLS.Enabled {
		sb.WriteString("&encrypt=true")
		if c.TLS.InsecureSkipVerify {
			sb.WriteString("&TrustServerCertificate=true")
		}
		if c.TLS.CAFile != "" {
			sb.WriteString("&certificate=")
			sb.WriteString(url.QueryEscape(c.TLS.CAFile))
		}
	}
	return sb.String()
}
