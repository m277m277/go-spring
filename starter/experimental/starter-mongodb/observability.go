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

package StarterMongoDB

import (
	"context"
	"strconv"
	"sync"

	"go.mongodb.org/mongo-driver/v2/event"
	observe "go-spring.org/observe"
)

// newCommandMonitor returns an event.CommandMonitor that drives the shared
// observe kit for every MongoDB command: a trace span, a duration/in-flight
// metric, and an access log (off/brief/detailed). A command's observation is
// opened in Started and closed in Succeeded/Failed; events are correlated by
// (connection id, request id), which the driver guarantees is unique for an
// in-flight command.
//
// Why hand-rolled against the v2 event API (not otelmongo): the official
// otelmongo instrumentation targets the v1 mongo driver and its CommandMonitor
// type is incompatible with the v2 driver this starter uses. The bridge here
// delegates the three signals to the observe kit so MongoDB shares the same
// vocabulary (db.client.operation.duration, db.system=mongodb, ...) as every
// other client starter.
func newCommandMonitor(obs *observe.Observer) *event.CommandMonitor {
	var inFlight sync.Map // spanKey -> *observe.Span

	return &event.CommandMonitor{
		Started: func(ctx context.Context, e *event.CommandStartedEvent) {
			_, sp := obs.Start(ctx, e.CommandName, e.DatabaseName)
			inFlight.Store(spanKey(e.ConnectionID, e.RequestID), sp)
		},
		Succeeded: func(_ context.Context, e *event.CommandSucceededEvent) {
			if v, ok := inFlight.LoadAndDelete(spanKey(e.ConnectionID, e.RequestID)); ok {
				v.(*observe.Span).End(nil)
			}
		},
		Failed: func(_ context.Context, e *event.CommandFailedEvent) {
			if v, ok := inFlight.LoadAndDelete(spanKey(e.ConnectionID, e.RequestID)); ok {
				v.(*observe.Span).End(e.Failure)
			}
		},
	}
}

// spanKey uniquely identifies an in-flight command by its connection and
// request id, so the Succeeded/Failed event can find the span Started opened.
func spanKey(connID string, requestID int64) string {
	return connID + "/" + strconv.FormatInt(requestID, 10)
}
