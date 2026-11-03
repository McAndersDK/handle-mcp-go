# handle-mcp

A local MCP server (stdio) for Danish grocery stores with home delivery. Goal and roadmap: the README. Findings about the stores: `docs/research.md`. How to contribute: `CONTRIBUTING.md`.

## Rules (never break)

1. **The human always pays.** No code may order, pay, read cards, choose a delivery time or change the address. See `docs/adr/0001-the-human-always-pays.md`. `tests/test_safety.py` greps for forbidden endpoints.
2. **Privacy first.** Every tool answer goes through `privacy.scrub()` in `server._out()`. Never return raw store JSON without it.
3. **stdout is JSON-RPC.** Never `print()`. Log to stderr with `logging`.
4. **No secrets in git.** Logins only in env vars, the OS keychain (`handle-mcp login <store>`) or `~/.config/handle-mcp/<store>.json` (chmod 600).
5. **Small and boring.** < ~550 lines per store file, < ~300 in every other file. No layer until two real users need it. A store that grows past the limit splits into mixins, `stores/<store>_<topic>.py`, never into a second layer.
6. **No Claude attribution** in commits, PRs or comments.

## Layout

```
src/handle_mcp/
  server.py        all tools, store selection, error handling
  cli.py           `handle-mcp login <store>`: logins into the OS keychain
  compare.py       compare_basket: best product per item, cheapest store
  candidates.py    the products per item the caller picks from, with flags
  match.py         words: does this product match the item?
  pricing.py       kroner: packs for the amount, offers, delivery day
  recipe_cart.py   add_recipe_to_cart: a recipe's products as absolute cart counts
  models.py        dataclasses; money is Decimal DKK
  privacy.py       scrub(): removes personal data
  stores/__init__.py  Store protocol, StoreError, load_login(), STORES
  stores/_http.py     one request with the retries every store needs
  stores/<store>.py   one file per store: login, calls and parsing for that store
  stores/<store>_<topic>.py  mixins when a store outgrows one file (nemlig_offers, nemlig_recipes)
tests/             no network, no logins
tests/snapshots/tools.json  every tool as the model sees it (name, description, args)
docs/adr/          one short file per hard decision
docs/tools.md      the tool table users read: stores per tool, what writes
```

## Language

- Code, comments, docs, commits and PRs: English.
- Everything a human reads in a tool answer or error: Danish. `StoreError("nemlig.com har kø lige nu.")`.
- Product names, offer texts and store messages pass through as the store wrote them (after `privacy.scrub()`).

## Tools

- Every tool has annotations: `READ`, `WRITE` (absolute cart quantities) or `ADD` (adds to the cart). Nothing else.
- Every write tool says "Never orders or pays" in its description. A tool that adds to the cart says "ASK THE HUMAN FIRST". The tool never asks; the model does.
- A tool that works in one store says so in its description and in `docs/tools.md`.
- `tests/snapshots/tools.json` is the contract. A change to a tool's name, description or args is on purpose, in its own commit, with `HANDLE_UPDATE_SNAPSHOT=1`.
- Tool answers are small, readable JSON: money as strings in kroner (`"12.95"`), dates ISO, no raw store fields. A missing fact is `null` with a reason next to it, not a guess.
- A new rule or a changed one gets a short ADR in `docs/adr/`.

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
- nemlig: the front page and `?GetAsJson=1` land in queue-it. Use `/webapi/` and the productbff API. Never work around a queue or a bot check: raise a clear StoreError and let the human wait.
- Fixtures are real captures with personal data replaced. `tests/fixtures/<store>/README.md` says what was captured and when, so a stale fixture is easy to spot.

## Test

```
uv sync
uv run pytest -q          # unit tests, no network
HANDLE_UPDATE_SNAPSHOT=1 uv run pytest tests/test_protocol.py   # after changing a tool's name, description or args on purpose
uv run ruff check . && uv run ruff format --check .
HANDLE_LIVE=1 uv run pytest -m live   # read-only smoke tests against real stores (from step 3)
HANDLE_LIVE=1 uv run pytest -m live -s tests/test_live_list.py   # 50 fixed items (tests/live_list.json): fails when no candidate fits
```

## Workflow

One PR per change. Run the tests before you push, show the result, wait for OK.

- A change that touches a store file: run the live tests for that store before merge. Stores change without notice.
- A change to the matcher (`match.py`, `pricing.py`, `candidates.py`): run the live list before merge.
- A new tool or a changed one: update `docs/tools.md` and the snapshot in the same PR.
- Squash and merge. The squash message is the PR title.

## Release

1. Bump `version` in `pyproject.toml` (`uv lock` follows) and every pin: `grep -rn "@v" README.md docs/` (today only `docs/security.md`).
2. Merge, then tag the merge commit on `main`: `git fetch origin main && git tag vX.Y.Z origin/main && git push origin vX.Y.Z`.
3. Semver: a tool's args or answer shape changes in a way old skills notice → major. New tool or field → minor. Fix → patch.

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
