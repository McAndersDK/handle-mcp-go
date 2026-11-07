# Testing

| Layer | Command | What it proves |
|---|---|---|
| Unit and adapter | `uv run pytest -q` | Our code reads real (recorded, scrubbed) store answers right. No network |
| MCP protocol | `tests/test_protocol.py` (part of the above) | The tools as Claude sees them: JSON, errors, annotations. `tests/snapshots/tools.json` holds every tool's description: update on purpose with `HANDLE_UPDATE_SNAPSHOT=1 uv run pytest tests/test_protocol.py` |
| Safety | `tests/test_safety.py` (part of the above) | No order, payment, slot, "add recipe" or "clear cart" endpoint in the code |
| Live, read only | `HANDLE_LIVE=1 uv run pytest -m live` | The stores still answer as we expect. Needs logins. Never writes to the cart |

## Run the live tests often

Stores change their APIs without notice. Run the live tests before a merge that touches a store,
and on a schedule if you can, e.g. a scheduled task in your own tool that runs
`HANDLE_LIVE=1 uv run pytest -m live -q` on weekdays. Live tests only read: they never write to the
cart, and they print short tracebacks without locals, so no login reaches a log.
