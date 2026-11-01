# handle-mcp

A local MCP server (stdio) for Danish grocery stores with home delivery. Full goal and roadmap: the project's `CLAUDE_CODE_PROMPT.md`. Findings about the stores: `docs/research.md`.

## Rules (never break)

1. **The human always pays.** No code may order, pay, read cards, choose a delivery time or change the address. See `docs/adr/0001-the-human-always-pays.md`. `tests/test_safety.py` greps for forbidden endpoints.
2. **Privacy first.** Every tool answer goes through `privacy.scrub()` in `server._out()`. Never return raw store JSON without it.
3. **stdout is JSON-RPC.** Never `print()`. Log to stderr with `logging`.
4. **No secrets in git.** Logins only in env vars or `~/.config/handle-mcp/<store>.json` (chmod 600).
5. **Small and boring.** < ~300 lines per store file, < ~200 in `server.py`. No layer until two real users need it.
6. **No Claude attribution** in commits, PRs or comments.

## Layout

```
src/handle_mcp/
  server.py        all tools, store selection, error handling
  models.py        dataclasses; money is Decimal DKK
  privacy.py       scrub(): removes personal data
  stores/__init__.py  Store protocol, StoreError, load_login(), STORES
  stores/<store>.py   one file per store
tests/             no network, no logins
docs/adr/          one short file per hard decision
```

## Add a store

1. Create `src/handle_mcp/stores/<name>.py` with a class that has every method in `stores.Store`.
2. Raise `StoreError("clear Danish message")` for expected failures. Raise `NotImplementedError` if the store cannot do something.
3. Add one line to `STORES` in `stores/__init__.py`.
4. Tests: fixtures in `tests/fixtures/<name>/` are real answers with personal data replaced. Mock HTTP with `httpx.MockTransport`.

No change to `server.py` or the tools is needed.

## Store API lessons (see docs/research.md)

- Money from stores may be in øre (Bilka). Convert to Decimal kroner in the adapter.
- Quantities are absolute; 0 removes the line.
- Bilka answers HTTP 200 on errors. A cart change failed if the applied count differs from the requested one.
- Bilka: log in without a `cart` cookie, or the login replaces the cart.
- Retry only on timeout, network errors (e.g. ReadError), 429 and 5xx. Safe because every write sets an absolute quantity.

## Test

```
uv sync
uv run pytest -q          # unit tests, no network
uv run ruff check . && uv run ruff format --check .
HANDLE_LIVE=1 uv run pytest -m live   # read-only smoke tests against real stores (from step 3)
```

## Workflow

Build in the steps from `CLAUDE_CODE_PROMPT.md`. After each step: run the tests, show the result, wait for OK.

## Branches, commits and PRs (gitmoji)

Branch name = type + short slug. The type says what the branch solves:

| Type | Gitmoji | Use for |
|---|---|---|
| `feature/` | ✨ | new tool, store or behaviour |
| `fix/` | 🐛 | bug fix |
| `hotfix/` | 🚑️ | urgent fix on main |
| `refactor/` | ♻️ | same behaviour, better code |
| `docs/` | 📝 | docs only |
| `test/` | ✅ | tests only |
| `chore/` | 🔧 | config, CI, dependencies |

Example: branch `feature/compare-basket`, PR title `✨ Compare a basket across stores`.

- Start every commit message and PR title with the gitmoji ([gitmoji.dev](https://gitmoji.dev)).
- No emoji in branch names: they break shells and URLs. The prefix is enough.
- One type per branch. Split mixed work into two branches.
