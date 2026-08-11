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

package StarterGormPostgres

import (
	"context"
	"testing"
	"time"

	"go-spring.org/spring/gs"
)

// TestNewClientPlainDSN is the regression test for the plain-DSN (non-discovery)
// open path. Previously newClient only opened the gorm.DB inside the discovery
// branch, leaving db nil in plain-DSN mode and panicking in applyPool when it
// called gorm.DB.DB() on a nil receiver. With the fix the plain-DSN branch opens
// the DB itself, so an unreachable postgres must surface as a normal connection
// error rather than a nil-pointer panic.
func TestNewClientPlainDSN(t *testing.T) {
	ctx := &gs.ContextProvider{Context: context.Background()}
	c := Config{
		// Port 1 on loopback is closed, so the open + startup ping fails fast.
		Host:        "127.0.0.1",
		Port:        "1",
		User:        "nobody",
		Password:    "nope",
		DB:          "nodb",
		SSLMode:     "disable",
		PingTimeout: 500 * time.Millisecond,
	}

	client, err := newClient(ctx, c)
	if err == nil {
		t.Fatal("expected a connection error for unreachable postgres, got nil (plain-DSN must not panic)")
	}
	if client != nil {
		t.Fatalf("expected nil client on failure, got %v", client)
	}
}
