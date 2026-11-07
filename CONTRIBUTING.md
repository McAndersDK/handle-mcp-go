# Contributing

Small and boring wins. Read `CLAUDE.md` first: the rules, the layout and how to add a store.

## Setup

```sh
go test -count=1 ./...                 # no network, no logins
go vet ./... && gofmt -l cmd internal  # gofmt must print nothing; gofmt -w cmd internal fixes it
go test -race -count=1 ./...           # what CI runs (needs a C compiler, CGO_ENABLED=1)
```

Live tests (`HANDLE_LIVE=1 go test -count=1 -v -run Live ./internal/store/<store>/`) call the real stores with **your own** accounts, saved with `handle-mcp login <store>`. They only read. CI never runs them. See [docs/testing.md](docs/testing.md).

## Add a store

1. Create a package `internal/store/<name>/` with a type that has every method of the `Store` interface in `internal/store/store.go`.
2. Return `store.Errorf("clear Danish message")` for expected failures, and `store.ErrNotImplemented` for what the store cannot do.
3. Register it in the map in `internal/server/registry.go`.
4. Tests: real, redacted answers in `tests/fixtures/<name>/`, HTTP mocked with a fake `http.RoundTripper`. No socket is opened.

No change to the server or the tools is needed. Store facts: [docs/research.md](docs/research.md).

## Commits and pull requests

This repo is a permanent fork of `NJohansen/handle-mcp`. Normal work is pushed straight to `main` of the fork. A PR is used only to release a new version: `gh pr create --repo McAndersDK/handle-mcp-go`. Never open a PR or push to `upstream`.

- Branches are not required. If you use one: `<type>/<slug>`, type is one of `feature`, `fix`, `hotfix`, `refactor`, `docs`, `test`, `chore`. One type per branch.
- Commit messages and PR titles are one short imperative sentence, with no emoji. No emoji anywhere in the repo.
- Tests for new code. HTTP is mocked with a fake `http.RoundTripper`. Run them before each push.
- Fixtures are real store answers with personal data replaced. Say so in `tests/fixtures/<store>/README.md`. Never commit a login, a cookie, a token or a real name, address, phone or email.

## Never

- An endpoint that orders, pays, reads cards, chooses a delivery time or changes the address. `internal/safety/safety_test.go` fails if one appears.
- Writing to stdout outside the MCP stream (`fmt.Print`, `os.Stdout`). stdout is the MCP stream. Log to stderr with `log/slog`.
- Raw store JSON in a tool answer. Every answer leaves through `internal/server/out.go`, which scrubs it.
