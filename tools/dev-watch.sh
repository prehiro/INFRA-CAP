#!/usr/bin/env bash
# INFRA-CAP dev watcher — auto rebuild/restart on template/css/go changes.
# Zero dependencies (uses inotifywait if present, else polling fallback).
set -u
cd "$(dirname "$0")/.."

GO=tools/go/bin/go
LOG=/tmp/infracap.log

restart() {
  pkill -f "exe/infracap-server" 2>/dev/null || true
  pkill -f "go run ./cmd/server" 2>/dev/null || true
  sleep 0.3
  make sync-templates >/dev/null
  echo "[$(date +%H:%M:%S)] restarting..."
  nohup bash -c "set -a && . ./.env && set +a && exec $GO run ./cmd/server" > "$LOG" 2>&1 &
  echo "[$(date +%H:%M:%S)] up on http://localhost:1112"
}

# initial start
restart

if command -v inotifywait >/dev/null 2>&1; then
  echo "[watch] using inotifywait"
  while true; do
    inotifywait -q -e modify,create,move,delete \
      -r web/templates web/static/css web/tailwind.css \
      cmd internal migrations 2>/dev/null
    make css >/dev/null 2>&1 || true
    restart
    sleep 0.5   # debounce burst of events
  done
else
  echo "[watch] inotifywait not found — falling back to 2s polling"
  sig() { find web/templates cmd internal migrations web/tailwind.css -type f -newer /tmp/.infracap-watch-marker 2>/dev/null | head -1; }
  touch /tmp/.infracap-watch-marker
  while true; do
    sleep 2
    if [ -n "$(sig)" ]; then
      make css >/dev/null 2>&1 || true
      restart
      touch /tmp/.infracap-watch-marker
    fi
  done
fi
