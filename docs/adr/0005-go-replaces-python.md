# 0005: Go replaces Python

handle-mcp-go is a permanent fork of NJohansen/handle-mcp. Go replaces Python. Upstream fixes are ported by hand.

- Module path: `github.com/McAndersDK/handle-mcp-go`. The binary stays `handle-mcp`.
- `go.mod` is at the repo root and the Go code is in `cmd/` and `internal/`, so the module keeps plain tags like `v1.0.0`. The Python code lives on in `upstream/`, a mirror of upstream's tree.
- Distribution: GoReleaser binaries on GitHub Releases, plus `go install github.com/McAndersDK/handle-mcp-go/cmd/handle-mcp@v1.0.0`.
- The first release is `v1.0.0`, on a new numbering line. Origin had no tags when this was checked on 2026-11-05. From then on the semver rule in `CLAUDE.md` applies unchanged. A published tag is never moved.
- The six rules in `CLAUDE.md` keep their content. `LICENSE` keeps the upstream copyright.

## Why

The maintainer does not want Python. That is the reason. The facts that follow from it: one binary, and no uv or Python needed to run it.

## Contract

- Tool names, descriptions and args stay.
- Answers match Python as parsed JSON. Key order may become alphabetical.
- Each answer is one text content with 2-space-indented JSON and no `structuredContent`.

Deliberate differences from Python 1.2.1:

- `idempotentHint: false` is present on READ tools. False is the MCP default, so the meaning is unchanged.
- `_meta` with `io.modelcontextprotocol/serverInfo` is present on results.
- The English prefix `Error executing tool <name>: ` is dropped.
- Argument errors are Danish and never echo the value.
- `true` is refused as an integer. A boolean must not set a cart quantity.
- Money in free text always has two decimals (the spirit of ADR 0002), e.g. `278.00 kr. mangler til 300.00 kr.`
- The `notes` key is scrubbed as free text. Python's `FREE_TEXT_KEYS` in `privacy.py` lacks it; the gap is fixed only in Go.
- Free-text addresses also match definite-form street names (`-vejen`, `-gaden`, `-boulevarden` ...), e.g. `Søndergaden 5`. Python misses them, which leaks an address; the gap is fixed only in Go.
- Odd numeric strings are refused as integers (see Arguments).
- A refused login during search (wrong email or password, or a dead saved session) is tried once, not twice, because repeated attempts can lock an account. Python swallowed the failure in the settings step and `search` logged in again. A network failure or a 5xx during login in that step is still tried once more by `search`.
- A missing price in search, offers or recipes is a format-change error, not 0.00 kr, because that would read as a free product. An explicit price of 0 is still 0.00 kr.
- Fractions (`1/2`, `2/3`, `½`, `1½`, `1 1/2`) are read as amounts, e.g. `1/2 dl` is 50 ml. Python read `1/2 dl` as 2 dl (the number after the slash won). A slash before an amount (`/2 dl`) and a denominator of 0 give no size.
- nemlig hides a chosen delivery slot that has ended (`chosen_start`, `chosen_end` and `edit_deadline` are `null`, the note says no time is chosen), as BilkaToGo does, because a past delivery day would count expired offers as active. Python passed the stale slot through.
- A missing unit price is `null`, not 0.00 kr per unit, which would sort the product first as the cheapest.
- The BilkaToGo recipe page reader accepts a small subset of JavaScript literals. It never turns an unknown name into a string: a name, a function or a page nested deeper than 200 levels is the format-change error "BilkaToGo-siden har ændret format.".
- A recipe link to another host is refused, and Bilka item numbers go into an Algolia filter only as ASCII digits.
- NaN money becomes a `StoreError`, because `shopspring/decimal` cannot represent it. This amends ADR 0002 ("shown as it is"). ADR 0002 itself is not edited.
- BilkaToGo pickup carts are supported, with `delivery_mode` and `pickup_store` on four answers, a fail-closed mode rule and ten named golden exceptions. Python treats every non-home cart as `NOT_HOME`. See ADR 0006.
- The matcher reads the name word `hk` as `hakket`, because REMA 1000 writes `HK. GRISEKØD`. A small abbreviation table in `internal/match` applies to whole words only and never changes a displayed name. No golden contains the word, so there are no golden exceptions.
- The matcher finds a blend share (a number, `%`, a food word) anywhere in the name, not only after a flavour marker, so `HK. OKSEKØD, 35% GRØNT` is a blend for `hakket oksekød`. Nutrients, alcohol and `mælk fat` are skipped, and the item asking for that food cancels it. Python only looks after a marker. No golden changes, so there are no golden exceptions.

## Tools

- `internal/server/tools.json` is embedded with `go:embed` and is the source of truth. It is a copy, because embed cannot reach `tests/`.
- `tests/snapshots/tools.json` is the wire contract. It was regenerated from the Go server in its own commit before `v1.0.0`, and the Go test compares the live tool list with it exactly. The Python mirror in `upstream/` has its own copy and does not read this one.
- Tools are registered with the low-level `Server.AddTool` only.

## Arguments

A small hand-written decoder:

- A top-level string for a non-string field is parsed as JSON and replaced only if the result is an array, object or null. This copies the Python SDK's `pre_parse_json`; clients send lists this way.
- Integers accept the numbers `2` and `2.0` and the strings `"2"` and `" 2 "`. A string is accepted only as ASCII digits with optional surrounding whitespace and no leading zero, except `"0"` itself, because `0` removes a cart line.
- Everything else is refused: `2.5`, `"mange"`, `[]`, `{}`, `true`, and strings such as `"2_0"`, `"+2"`, `"02"`, `"2.0"`, `"-1"` and `"٢"`. A clear error beats a wrong quantity (Python reads `"2_0"` as 20).
- `tests/golden/wire/args.json` records Python's behaviour. The refused strings are named exceptions for the server phase.
- Negative numbers given as numbers are not this rule's business: they reach each tool's existing range check.
- A string field given a number is refused.
- `null` is accepted only for args whose schema allows it. It is refused for `limit`, `days`, `sort` and required args.
- Unknown keys are ignored.

## Privacy

Every tool answer has one exit. `FakeStore` in Python can smuggle dicts past `scrub()`; typed structs cannot, so four tests replace that check:

1. Single exit: a handler returns leaky data, plus an AST check that result construction appears only in the exit file.
2. A canary deep-merged into every fixture object and sent through the real adapters.
3. Free-text and error scrubbing.
4. A reflection test that classifies every answer key as passthrough, free text or personal.

## Money and time

- `shopspring/decimal`, rounding only when marshalling.
- ISO output is Python-compatible: a naive time stays naive.
- Wall-clock delivery times via `time.Date`.
- `time/tzdata` is embedded.

## Goldens

`tests/golden/` is made once by an agent from the unchanged Python code, offline: fixtures and mocked HTTP only, an in-memory keyring, a temp `HANDLE_CONFIG_DIR`, sockets blocked, no logins. The human runs nothing. The script stays in `scripts/` and runs against the `upstream/` mirror, so goldens can be re-exported after a merge (see 0007).

Goldens cannot prove:

- live store behaviour and logged-in calls
- Gigya, queue-it and bot checks
- the effect of nemlig's `DoMerge` login flag
- real session sizes and real keychains
- what MCP clients really send

## Python

Python is not deleted: it lives on in `upstream/` as a mirror of upstream's tree, never edited except for redactions, and never run by the human. See 0007.

## Gate

A read-only Go login check against both real stores must pass before phase 3 of the port (`model` and `privacy`). If a store blocks Go, the port stops and goes to the human. No TLS spoofing or other workaround: `CLAUDE.md` forbids working around a bot check.

## Verification

There is no Python live baseline, because no logins exist for it. A side-by-side run is replaced by:

- goldens green
- the ported Go live suite
- the Go live list
- a human acceptance run in a real MCP client, including one `set_cart_quantities` add and remove checked on the store's website. The existing live tests never write to a cart.

## Layout and rule text

```
cmd/handle-mcp
internal/{model,privacy,match,pricing,candidates,compare,recipecart,
                 server,cli,store,store/nemlig,store/bilkatogo}
```

The store registry lives outside `store`, to avoid an import cycle. Goldens stay in `tests/golden/`.

`CLAUDE.md` was rewritten for Go before `v1.0.0`:

- Rule 2: "Every tool answer goes through `privacy.Scrub()` in the one exit, `internal/server/out.go`. Never return raw store JSON without it."
- Rule 3: "stdout is JSON-RPC. Never use `fmt.Print*`, `println` or `os.Stdout`. Log to stderr with `log/slog`."
- Rule 5: keeps < ~550 lines per store file and < ~300 in every other file, excluding `_test.go`. "A store that grows past the limit splits into files by topic in its own package, never into a second layer."

## Logins

Env vars, the `<store>.json` file format, the `~/.config/handle-mcp` path on every OS and the session shape (ADR 0003) all stay.

Old keychain entries are not read. Python's Windows entries use target `handle-mcp` or `<store>@handle-mcp`; go-keyring uses `handle-mcp:<store>`. Users log in again.

## Consequences

- Python stays in `upstream/`, so two codebases exist side by side (0007).
- Upstream fixes need a manual port and a golden check.
- Users must log in again after the switch.

## Open

Not decided here.

- Nothing about the folder name: `src-go/` was dropped before `v1.0.0`, so the `go install` path is `.../cmd/handle-mcp`.
