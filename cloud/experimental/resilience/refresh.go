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

package resilience

import (
	"context"
	"reflect"
	"time"
)

// RefreshLoop periodically reads policy() and, when the returned [Policy]
// changes (reflect.DeepEqual), Refreshes exec. It blocks until ctx is done.
//
// Start it as a goroutine from a starter that binds its resilience policy via
// gs.Dync — gs has no per-key change callback, so a low-frequency poll (the
// same approach as dynamic timeout/dubbo refresh) is how a config change takes
// effect without a restart, within interval.
//
// interval <= 0 defaults to 10s. The first iteration seeds exec with the
// current policy (idempotent with the constructor). [Policy.RetryPredicate] is
// a func assigned in code, not config; as long as policy() returns the same
// func value each call (a dynamic refresh changes numeric thresholds, not the
// predicate), reflect.DeepEqual classifies it unchanged and only threshold
// edits trigger a Refresh.
func RefreshLoop(ctx context.Context, exec RefreshableExecutor, policy func() Policy, interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	cur := policy()
	_ = exec.Refresh(cur) // seed; idempotent if the executor was built from the same policy.
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p := policy()
			if reflect.DeepEqual(p, cur) {
				continue
			}
			if err := exec.Refresh(p); err == nil {
				cur = p
			}
		}
	}
}
