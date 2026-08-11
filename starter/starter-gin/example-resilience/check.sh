#!/usr/bin/env bash
#
# Smoke test for starter-gin's inbound resilience admission. Runs the example,
# which self-asserts that a burst is both served (200) and shed (429), and exits
# non-zero on failure. No external services or docker are required.
#
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

go run -gcflags="all=-N -l" . &
pid=$!
( sleep 40; kill -9 "${pid}" 2>/dev/null ) &
watchdog=$!
rc=0
wait "${pid}" 2>/dev/null || rc=$?
kill "${watchdog}" 2>/dev/null || true
wait "${watchdog}" 2>/dev/null || true
exit "${rc}"
