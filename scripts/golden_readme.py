"""The text of tests/golden/README.md."""

README = """\
# Golden files

What the Python code of handle-mcp answers, recorded as JSON. The Go port (ADR 0005) is tested
against these files. They are made once and kept; the script that made them is deleted at cutover.

- Made: 2026-11-05, from the Python code of commit `{commit}`, the last commit that changed
  `src/`, `tests/`, `pyproject.toml` or `uv.lock`. `git HEAD` when it ran: `{head}` (later commits
  are Go and CI). `manifest.json` has both, the library versions, the pinned clock and the sha256
  of every fixture, Python source file and golden file.
- Command: `uv run --frozen python -m scripts.export_golden`, from the repo root. Run twice, the
  files are byte for byte the same.
- Offline: fixtures and `httpx.MockTransport` only. A temp `HANDLE_CONFIG_DIR`, an in-memory
  keyring, every `HANDLE_*` variable removed, `socket.connect` blocked, no logins, no store calls.
  Fake sessions exist only in the memory keyring: `{{".ASPXAUTH": "auth-1"}}`, a fake `login_token`
  and the old `{{"email", "password": "hemmeligt"}}`.
- Format: UTF-8, LF, sorted keys, `ensure_ascii=False`, indent 1 (`.gitattributes` keeps LF).

## Files

| Folder | What |
|---|---|
| `privacy/` | `scrub.json`, `scrub_text.json`, `keys.json` (PERSONAL_KEYS, FREE_TEXT_KEYS and key spellings), `money.json` (`jsonable`) |
| `match/` | `corpus_<corpus>.json` (the products), `parse.json` (items, sizes, per product facts), `pairs_<corpus>.json` |
| `compare/` | `best_match_<corpus>.json`, `quote.json`, `verdict.json`, `search_sort.json` |
| `recipecart/` | `plan.json` (plan, in_cart, report) |
| `stores/<store>/<case>.json` | one adapter call (or a few): method, args, login, requests, answer or error |
| `wire/` | `session.json` (a real stdio subprocess), `args.json` (accepted and refused argument values) |

Corpora: `nemlig` and `bilkatogo` are products parsed by the real adapters from `tests/fixtures`;
`synthetic` is the hand-made products of the tests. Items come from `tests/live_list.json`,
`WEEK_LIST`, every item literal in `tests/test_compare.py` and adversarial strings (`×`, non-breaking
spaces, Arabic-Indic digits, upper case Danish letters).

## Reading them

- **Pairs are sparse.** `match/pairs_<corpus>.json` has a row only when the score is above 0.
  Go must assert score 0 for every (item, product) pair that is not listed. `p` is the index into
  `corpus_<corpus>.json`. `by_day` has `cost` and `per_unit` (`[kind, value]`) for the delivery
  days `null`, `2026-11-02` and `2030-01-01` (2030 expires every offer).
- **Answer money** (`compare/`, `wire/`, `stores/`, `privacy/money.json`) is Python's two-decimal
  string (`"12.95"`, `"39.00"`). **Unit-level numbers** in `match/` are plain strings with no
  trailing zeros (`"1000"`, `"12.95"`); `per_unit` is rounded half up to 10 places. **Corpus inputs**
  keep the exact `str(Decimal)`.
- Dates are `isoformat()` strings: `+01:00` for Copenhagen time, a naive time stays naive.
- Store answers (`stores/`) are the adapter result through `jsonable()`, before `scrub()`.
- `requests[]` lists method, URL without query, `params`, `body`, chosen headers and cookie names.
  `X-Correlation-Id` is left out. Delays in retries are removed and the clock is pinned (below).
- `wire/session.json`: each call has the whole JSON-RPC `response`, `text` (the raw text of the
  result), and `text_without_prefix` (Python's `Error executing tool <name>: ` removed). Python's
  own argument errors have `text_compared: false`: the text is kept, Go's is Danish and different.
- `wire/args.json`: per value `accepted`, `received` (what the store saw, after clamping) or
  `error`. Exotic numeric strings follow what is recorded there (ADR 0005).

## Pinned time

`datetime` in `handle_mcp.stores.bilkatogo` answers `now()` = `2026-11-02T12:00:00+01:00`.
nemlig's settings cache uses `time.monotonic()`; it is replaced by a counter the case can advance
(600 s TTL). The retry sleep does nothing. Delivery days 2026-03-29 and 2026-10-25 (daylight
saving) are in `stores/*/slots_dst_*.json` because the slot code adds hours to a wall-clock date.

## Normalised text

Python prints Decimal exponents and raw precision in free text; Go always prints two decimals
(ADR 0005). Exactly three templates are rewritten to two decimals, in every file, and nothing else
(`19-51 kr.` in `nemlig.py` is untouched):

1. `Under minimumskøb: X kr. mangler til Y kr.` (`compare.py`)
2. `N mere for X kr.` (`pricing.py`)
3. `Billigst: S (X kr. inkl.` (`compare.py`)

`manifest.json` counts how many strings each rewrite changed.

## What the goldens do not prove

- live store behaviour, and logged-in calls (every answer is a fixture)
- Gigya, queue-it and bot checks (they are scripted fake answers)
- the effect of nemlig's `DoMerge` login flag
- real session sizes and real keychains
- what real MCP clients send (the wire client is a small script; protocol `{protocol}`)

Python's behaviour on odd input (a crash, an odd message) is recorded where the cases hit it.
That is not a promise that Go must copy a Python accident.
"""
