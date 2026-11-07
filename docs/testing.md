# Testing

| Layer | Command | What it proves |
|---|---|---|
| Unit and adapter | `go test -count=1 ./...` | Our code reads real (recorded, scrubbed) store answers right. No network. HTTP is mocked with a fake `http.RoundTripper` |
| MCP protocol | `TestToolSurface` in `internal/server` (part of the above) | The tools as Claude sees them: JSON, errors, annotations. `tests/snapshots/tools.json` holds every tool's description: update on purpose with `HANDLE_UPDATE_SNAPSHOT=1 go test ./internal/server -run TestToolSurface` |
| Safety | `internal/safety/safety_test.go` (part of the above) | No order, payment, slot, "add recipe" or "clear cart" endpoint in the code |
| Live, read only | `HANDLE_LIVE=1 go test -count=1 -v -run Live ./internal/store/<store>/` | The stores still answer as we expect. Needs logins. Never writes to the cart |
| Live list | `HANDLE_LIVE=1 go test -count=1 -v -timeout 30m -run Live ./internal/livelist/` | 50 fixed items: fails when no candidate fits |

## Run the live tests often

Stores change their APIs without notice. Run the live tests before a merge that touches a store,
and on a schedule if you can, e.g. a scheduled task in your own tool that runs the live commands above on weekdays. Live tests only read: they never write to the cart.

## CI

CI runs only on release PRs, or by hand (`gh workflow run ci --repo McAndersDK/handle-mcp-go`). It runs `go vet ./...`, checks `gofmt` on Ubuntu, and runs `go test -race ./...` on Ubuntu, Windows and macOS. Run the unit tests locally before each push.
