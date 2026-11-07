# handle-mcp

A local MCP server (stdio) for Danish grocery stores with home delivery or pickup. Goal and roadmap: the README. Findings about the stores: `docs/research.md`. How to contribute: `CONTRIBUTING.md`.

## Rules (never break)

1. **The human always pays.** No code may order, pay, read cards, choose a delivery time or change the address. Choosing a pickup store or a pickup time, or switching between home and pickup, counts as changing the address (`docs/adr/0006-pickup-is-a-delivery-mode.md`). See `docs/adr/0001-the-human-always-pays.md`. `internal/safety` greps for forbidden endpoints.
2. **Privacy first.** Every tool answer goes through `privacy.Scrub()` in the one exit, `internal/server/out.go`. Never return raw store JSON without it.
3. **stdout is JSON-RPC.** Never use `fmt.Print*`, `println` or `os.Stdout`. Log to stderr with `log/slog`.
4. **No secrets in git.** Logins only in env vars, the OS keychain (`handle-mcp login <store>`) or `~/.config/handle-mcp/<store>.json` (chmod 600).
5. **Small and boring.** < ~550 lines per store file, < ~300 in every other file, `_test.go` excluded. No layer until two real users need it. A store that grows past the limit splits into files by topic in its own package, never into a second layer.
6. **No Claude attribution** in commits, PRs or comments.

## Layout

```
cmd/handle-mcp/       main: serves MCP over stdio, or runs login and logout
internal/
  server/             all tools, store selection, error handling; out.go is the one exit
  server/tools.json   the tool list, embedded; the source of the snapshot
  server/registry.go  every store, one line each
  cli/                `handle-mcp login <store>`: logins into the OS keychain
  compare/            compare_basket: best product per item, cheapest store
  candidates/         the products per item the caller picks from, with flags
  match/              words: does this product match the item?
  pricing/            kroner: packs for the amount, offers, delivery day
  recipecart/         add_recipe_to_cart: a recipe's products as absolute cart counts
  model/              the shapes every store returns; money is exact kroner
  privacy/            Scrub(): removes personal data
  pystr/              the few string rules where Python and Go differ
  store/              Store interface, StoreError, logins, one HTTP request with retries
  store/jv/           reads store JSON loosely
  store/<store>/      one package per store: login, calls and parsing for that store
  goldentest/         reads tests/golden for the Go tests
  livelist/           the live list: 50 fixed items priced in every store
  safety/             tests that keep rule 1 and the privacy rules
scripts/              the golden export tool (Python, runs against upstream/)
upstream/             upstream's Python tree, location data redacted; never built or edited
tests/
  fixtures/<store>/   real captures with personal data replaced
  golden/             what the Python code answers, recorded as JSON (ADR 0005)
  snapshots/tools.json  every tool as the model sees it (name, description, args)
  live_list.json      the 50 items of the live list
docs/adr/             one short file per hard decision
docs/tools.md         the tool table users read: stores per tool, what writes
.goreleaser.yaml      release builds
.github/workflows/    ci and release
```

## Language

- Code, comments, docs, commits and PRs: English.
- Everything a human reads in a tool answer or error: Danish. `store.Errorf("nemlig.com har kø lige nu.")`.
- Product names, offer texts and store messages pass through as the store wrote them (after `privacy.Scrub()`).

## Tools

- Every tool has annotations: `READ`, `WRITE` (absolute cart quantities) or `ADD` (adds to the cart). Nothing else.
- Every write tool says "Never orders or pays" in its description. A tool that adds to the cart says "ASK THE HUMAN FIRST". The tool never asks; the model does.
- A tool that works in one store says so in its description and in `docs/tools.md`.
- `tests/snapshots/tools.json` is the contract. A change to a tool's name, description or args is on purpose, in its own commit: edit `internal/server/tools.json`, then run `HANDLE_UPDATE_SNAPSHOT=1 go test ./internal/server -run TestToolSurface`.
- Tool answers are small, readable JSON: money as strings in kroner with two decimals (`"12.95"`, `"39.00"`, never `"39"` or `"0.0"`; see `docs/adr/0002-money-has-two-decimals.md`), dates ISO, no raw store fields. A missing fact is `null` with a reason next to it, not a guess.
- A new rule or a changed one gets a short ADR in `docs/adr/`.

## Add a store

1. Create the package `internal/store/<name>` with a type that has every method in `store.Store`. Split it into files by topic when it nears the limit.
2. Return `store.Errorf("clear Danish message")` for expected failures. Return `store.ErrNotImplemented` if the store cannot do something.
3. Add one line to `internal/server/registry.go`.
4. Fixtures in `tests/fixtures/<name>/` are real answers with personal data replaced, with a `README.md`. Mock HTTP in the tests. Add goldens if the Python mirror has the store, or hand-written tests if not. A store also gets live tests (`live_test.go`).

No change to the tools is needed.

## Store API lessons (see docs/research.md)

- Money from stores may be in øre (Bilka). Convert to exact kroner in the adapter.
- Quantities are absolute; 0 removes the line.
- Bilka answers HTTP 200 on errors. A cart change failed if the applied count differs from the requested one.
- Bilka: log in without a `cart` cookie, or the login replaces the cart.
- Retry only on timeout, network errors, 429 and 5xx (`internal/store/http.go`). Safe because every write sets an absolute quantity.
- nemlig: the front page and `?GetAsJson=1` land in queue-it. Use `/webapi/` and the productbff API. Never work around a queue or a bot check: return a clear `StoreError` and let the human wait.
- Fixtures are real captures with personal data replaced. `tests/fixtures/<store>/README.md` says what was captured and when, so a stale fixture is easy to spot.

## Test

```
go vet ./...
gofmt -l cmd internal                  # prints nothing when formatted (a CRLF checkout on Windows lists every file: ignore that)
go test -count=1 ./...                 # unit tests, no network
go test -race ./...                    # CI only, because it needs gcc
HANDLE_UPDATE_SNAPSHOT=1 go test ./internal/server -run TestToolSurface   # after changing a tool's name, description or args on purpose
HANDLE_LIVE=1 go test -count=1 -v -run Live ./internal/store/<store>/     # read-only smoke tests against one real store
HANDLE_LIVE=1 go test -count=1 -v -timeout 30m -run Live ./internal/livelist/   # 50 fixed items (tests/live_list.json): fails when no candidate fits
uv run --frozen --project upstream python -m scripts.export_golden   # re-export the goldens from the mirror
```

## Merging upstream

`upstream` is `NJohansen/handle-mcp`. Its whole tree lives in `upstream/` (`docs/adr/0007-upstream-lives-in-a-mirror-folder.md`).

```
git fetch upstream
git merge -X subtree=upstream upstream/main
```

Use the default git config. Conflicts show only on the redacted lines: keep the redaction. Then port the change to Go by hand, re-export the goldens with the command under Test and read the golden diff. Never build, test or edit the mirror, except for redactions.

## Workflow

This repo is a permanent fork: `origin` is `McAndersDK/handle-mcp-go`, `upstream` is `NJohansen/handle-mcp`.

Normal work is pushed straight to `main` of the fork. No PR, no release, and a branch is not required. Run the tests locally before each push and show the result. CI runs only on release PRs, or by hand with `gh workflow run ci --repo McAndersDK/handle-mcp-go`.

- A change that touches a store package: run the live tests for that store before you push. Stores change without notice.
- A change to the matcher (`internal/match`, `internal/pricing`, `internal/candidates`): run the live list before you push.
- A new tool or a changed one: update `docs/tools.md` and the snapshot in the same commit.
- Never open a PR or push anything to `upstream`. We only pull from it. Upstream changes are merged into the mirror and ported to Go by hand.

## Release

A PR inside the fork is used only to release a new version. The version comes from the tag (GoReleaser sets `main.version`); there is no version file to bump.

1. Bump every `@v` pin: `grep -rn "@v" README.md docs/`.
2. Open the release PR in the fork with `gh pr create --repo McAndersDK/handle-mcp-go` (`gh` in a fork can default to the parent). CI runs on it. The squash message is the PR title.
3. Squash and merge.
4. Tag the merge commit and push the tag: `git fetch origin main && git tag vX.Y.Z origin/main && git push origin vX.Y.Z`. GoReleaser builds and publishes the release (`.github/workflows/release.yml`). A published tag is never moved.
5. Semver: a tool's args or answer shape changes in a way old skills notice -> major. New tool or field -> minor. Fix -> patch.

## Branches and commits

No emoji anywhere: not in commits, PR titles, branch names, code, docs or tool answers.

Branches are not required for normal work. When you use one (for a release PR or a long change), the name is type + short slug. The type says what the branch solves:

| Type | Use for |
|---|---|
| `feature/` | new tool, store or behaviour |
| `fix/` | bug fix |
| `hotfix/` | urgent fix on main |
| `refactor/` | same behaviour, better code |
| `docs/` | docs only |
| `test/` | tests only |
| `chore/` | config, CI, dependencies |

Example: branch `chore/release-v1.3.0`, PR title `Release v1.3.0`.

- A commit message or PR title is one short sentence in the imperative: "Port the nemlig store to Go".
- One type per branch and per commit. Split mixed work in two.
