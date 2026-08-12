#!/usr/bin/env bash
# Load test for starter-bigcache. BigCache is an embedded in-process cache, so
# there is no docker-compose / TCP port to wait on — just run the binary and let
# it self-exit after the load window. Toggle fault.* in conf/app.properties to
# inject failures.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

go run . &
pid=$!
( sleep 60; kill -9 "${pid}" 2>/dev/null ) &
watchdog=$!
rc=0
wait "${pid}" 2>/dev/null || rc=$?
kill "${watchdog}" 2>/dev/null || true
wait "${watchdog}" 2>/dev/null || true
exit "${rc}"
