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

package tcc

import (
	"context"
	"sync"
)

// txIDKey is the context key carrying a transaction id set by [WithTransactionID].
type txIDKey struct{}

// WithTransactionID returns a context that carries id, so [GlobalTCC] can pick up
// the transaction's idempotency key from the request context instead of the
// business method inventing one. Set it at the edge (a middleware deriving it
// from a request/idempotency header). The same id must key each participant's
// reservation so idempotence, empty rollback and anti-hanging can be detected.
func WithTransactionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, txIDKey{}, id)
}

// TransactionIDFromContext returns the transaction id previously stored by
// [WithTransactionID], and whether one was present.
func TransactionIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(txIDKey{}).(string)
	return id, ok
}

// ParticipantRegistry maps a logical method name to the ordered [Participant]s
// that make up its TCC transaction. It is the explicit, no-reflection equivalent
// of a @GlobalTransactional(type = TCC) declaration: business code registers a
// method's participants once at wiring time, and [GlobalTCC] looks them up by the
// joinpoint's method name. It is safe for concurrent use.
type ParticipantRegistry struct {
	mu    sync.RWMutex
	parts map[string][]Participant
}

// NewParticipantRegistry returns an empty registry.
func NewParticipantRegistry() *ParticipantRegistry {
	return &ParticipantRegistry{parts: make(map[string][]Participant)}
}

// Register associates method with participants, replacing any previous
// registration for the same method. participants is copied so later caller
// mutations do not alias the registry.
func (r *ParticipantRegistry) Register(method string, participants ...Participant) {
	cp := append([]Participant(nil), participants...)
	r.mu.Lock()
	if r.parts == nil {
		r.parts = make(map[string][]Participant)
	}
	r.parts[method] = cp
	r.mu.Unlock()
}

// Lookup returns the participants registered for method and whether any were
// found.
func (r *ParticipantRegistry) Lookup(method string) ([]Participant, bool) {
	r.mu.RLock()
	parts, ok := r.parts[method]
	r.mu.RUnlock()
	return parts, ok
}

// GlobalTCC returns the TCC @GlobalTransactional equivalent as a plain
// decorator: when method has participants registered in reg, it runs them as a
// [Transaction] through coord instead of calling proceed; when it does not, it
// calls proceed transparently. This keeps the wiring a no-op for un-declared
// methods and does not parse or reflect over the business call.
//
// The transaction id is taken from the context ([WithTransactionID]); absent
// that, the method name is used as a last resort, which is only correct for a
// single in-flight instance and is why callers are expected to set an explicit
// id.
//
// There is deliberately no shared interceptor-chain protocol: the decorator is
// an ordinary function, and combining cross-cutting concerns is ordinary
// nesting (see [transaction.GlobalTransactional] for the composed example).
func GlobalTCC(coord Coordinator, reg *ParticipantRegistry) func(ctx context.Context, method string, proceed func(context.Context) error) error {
	return func(ctx context.Context, method string, proceed func(context.Context) error) error {
		parts, ok := reg.Lookup(method)
		if !ok {
			return proceed(ctx)
		}
		id, ok := TransactionIDFromContext(ctx)
		if !ok {
			id = method
		}
		_, err := coord.Execute(ctx, Transaction{ID: id, Method: method, Participants: parts})
		return err
	}
}
