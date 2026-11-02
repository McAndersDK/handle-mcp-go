# Contributing

Small and boring wins. Read `CLAUDE.md` first: the six rules, the layout and how to add a store.

## Setup

```sh
uv sync
uv run pytest -q                                   # no network, no logins
uv run ruff check . && uv run ruff format --check .
```

Live tests (`HANDLE_LIVE=1 uv run pytest -m live -s`) call the real stores with **your own** accounts, saved with `handle-mcp login <store>`. They only read. CI never runs them.

## Pull requests

- Branch: `<type>/<slug>`, type is one of `feature`, `fix`, `hotfix`, `refactor`, `docs`, `test`, `chore`. One type per branch.
- Commit messages and PR title start with the matching gitmoji (✨ 🐛 🚑️ ♻️ 📝 ✅ 🔧).
- Tests for new code. HTTP is mocked with `httpx.MockTransport`.
- Fixtures are real store answers with personal data replaced. Say so in `tests/fixtures/<store>/README.md`. Never commit a login, a cookie, a token or a real name, address, phone or email.
- A Claude review runs on PRs from this repo. It is a second pair of eyes, not the decision.

## Never

- An endpoint that orders, pays, reads cards, chooses a delivery time or changes the address. `tests/test_safety.py` fails if one appears.
- `print()`. stdout is the MCP stream. Log to stderr with `logging`.
- Raw store JSON in a tool answer. Everything goes through `privacy.scrub()`.
