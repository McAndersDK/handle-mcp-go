#!/bin/sh
# Runs the read-only live tests against the real stores and shows a macOS notification when
# one fails. Logins come from the OS keychain (handle-mcp login <store>), never from here.
# Started by launchd on weekdays: see docs/testing.md.
set -u
cd "$(dirname "$0")/.." || exit 1
LOG_DIR="${HANDLE_LOG_DIR:-$HOME/Library/Logs/handle-mcp}"
mkdir -p "$LOG_DIR"
LOG="$LOG_DIR/live-$(date +%Y-%m-%d).log"
find "$LOG_DIR" -name 'live-*.log' -mtime +30 -delete 2>/dev/null

# Live runs get short tracebacks and no locals (tests/conftest.py), so no login reaches the log.
HANDLE_LIVE=1 uv run pytest -m live -q -p no:cacheprovider >"$LOG" 2>&1
status=$?

if [ "$status" -ne 0 ]; then
  failed=$(grep '^FAILED ' "$LOG" | sed -e 's/^FAILED [^:]*::\([^ ]*\).*/\1/' | head -5 | tr '\n' ' ')
  [ -n "$failed" ] || failed="pytest stoppede (exit $status)"
  # Text goes in as arguments, never into the AppleScript source: test names cannot break it.
  osascript -e 'on run argv
    display notification (item 1 of argv) with title "handle-mcp: live-test fejlede" subtitle (item 2 of argv)
  end run' "$failed" "Log: $LOG" || true
fi
exit "$status"
