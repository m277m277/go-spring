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

package StarterNeo4j

import (
	"context"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	observe "go-spring.org/observe"
)

// Why a kit-backed Query entry (not a transparent driver wrapper):
//
// neo4j-go-driver's ExecuteQuery is a package-level generic free function, not a
// method on DriverWithContext, so a driver wrapper cannot intercept the main
// query path. Rather than hand-roll a fragile wrapper around every session/
// transaction method (only to still miss ExecuteQuery), the starter exposes a
// Query helper that wraps ExecuteQuery with the observe kit. Applications that
// want trace+metric+log call StarterNeo4j.Query instead of neo4j.ExecuteQuery
// directly — one symbol swap, full instrumentation. The low-level StartSpan/
// EndSpan helpers remain for code that drives sessions manually.
//
// A package-level default Observer backs both (no per-instance LogConfig wiring,
// because the kit cannot bind to a free-function call path; the default level is
// "brief"). It rides the OTel globals starter-otel installs.

var defaultObs = observe.NewClient("neo4j", observe.LogConfig{Level: observe.DefaultBrief})

// Query runs a Cypher query via neo4j.ExecuteQuery, wrapped with the observe kit
// (trace span + duration/in-flight metric + access log). It is the instrumented
// drop-in for neo4j.ExecuteQuery: same signature, plus automatic observability.
func Query[T any](
	ctx context.Context,
	driver neo4j.DriverWithContext,
	query string,
	parameters map[string]any,
	newResultTransformer func() neo4j.ResultTransformer[T],
	settings ...neo4j.ExecuteQueryConfigurationOption,
) (T, error) {
	ctx, sp := defaultObs.Start(ctx, "query", query)
	res, err := neo4j.ExecuteQuery[T](ctx, driver, query, parameters, newResultTransformer, settings...)
	sp.End(err)
	return res, err
}

// StartSpan starts a client observation for a manual Neo4j operation (e.g. a
// session.Run / transaction callback the app drives directly). End the returned
// span once it completes. op names the operation; summary is the Cypher text
// (recorded as db.statement, bounded).
func StartSpan(ctx context.Context, op, summary string) (context.Context, *observe.Span) {
	return defaultObs.Start(ctx, op, summary)
}

// EndSpan records err (if any) on the span and ends it.
func EndSpan(span *observe.Span, err error) {
	span.End(err)
}
