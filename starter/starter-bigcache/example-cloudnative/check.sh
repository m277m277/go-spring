#!/usr/bin/env bash
#
# Smoke test for the starter-bigcache cloud-native flagship. bigcache is a purely
# in-process, zero-dependency cache, so this is fully SELF-CONTAINED: no docker,
# no external service. It runs the example (which self-asserts health, a
# Set/Get/Delete round-trip, resilience and dynamic config) under a 60s watchdog
# and propagates its exit code (0 = pass).
#
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

go run -gcflags="all=-N -l" . &
pid=$!
( sleep 60; kill -9 "${pid}" 2>/dev/null ) &
watchdog=$!
rc=0
wait "${pid}" 2>/dev/null || rc=$?
kill "${watchdog}" 2>/dev/null || true
wait "${watchdog}" 2>/dev/null || true
exit "${rc}"
