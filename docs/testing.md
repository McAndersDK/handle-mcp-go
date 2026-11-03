# Testing

| Layer | Command | What it proves |
|---|---|---|
| Unit and adapter | `uv run pytest -q` | Our code reads real (recorded, scrubbed) store answers right. No network |
| MCP protocol | `tests/test_protocol.py` (part of the above) | The tools as Claude sees them: JSON, errors, annotations. `tests/snapshots/tools.json` holds every tool's description: update on purpose with `HANDLE_UPDATE_SNAPSHOT=1 uv run pytest tests/test_protocol.py` |
| Safety | `tests/test_safety.py` (part of the above) | No order, payment, slot, "add recipe" or "clear cart" endpoint in the code |
| Live, read only | `HANDLE_LIVE=1 uv run pytest -m live` | The stores still answer as we expect. Needs logins. Never writes to the cart |

## Live tests every weekday morning (macOS)

`scripts/live-check.sh` runs the live tests and shows a notification listing the failed tests.
`scripts/live-check.plist` is a launchd LaunchAgent that starts it Monday to Friday at 09:00;
a run missed while the Mac sleeps starts when it wakes. Logins come from the keychain
(`handle-mcp login <store>`); nothing secret is in these files, and live runs print short
tracebacks without locals. Logs: `~/Library/Logs/handle-mcp/`.

Install, from the repo folder:

```bash
mkdir -p ~/Library/Logs/handle-mcp ~/Library/LaunchAgents
sed -e "s|REPO|$PWD|g" -e "s|HOME|$HOME|g" scripts/live-check.plist > ~/Library/LaunchAgents/handle-mcp.live-check.plist
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/handle-mcp.live-check.plist
```

Run it now: `launchctl kickstart gui/$(id -u)/handle-mcp.live-check`.
Remove: `launchctl bootout gui/$(id -u)/handle-mcp.live-check && rm ~/Library/LaunchAgents/handle-mcp.live-check.plist`.
