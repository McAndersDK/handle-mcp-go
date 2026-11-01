# handle-mcp

A local MCP server for Danish grocery stores with home delivery. An AI agent can search products, compare a shopping list across stores and fill the cart. **You always approve and pay yourself**, on the store's own website.

Stores in v1: [nemlig.com](https://www.nemlig.com) and [BilkaToGo](https://www.bilkatogo.dk) (home delivery).

> Unofficial. The stores have no public API. This uses the same private APIs as their websites, and it can break at any time. Not affiliated with nemlig.com or Salling Group.

## What it can do

| Tool | Writes? | What |
|---|---|---|
| `search_products(query, stores?)` | no | Price, price per kg/l, offers, EAN when known |
| `compare_basket(items, stores?)` | no | Prices a shopping list in each store: best match per item, fees, minimum order, unsure and missing items marked, cheapest store |
| `get_cart(store)` | no | Cart lines, fees, total, minimum order |
| `set_cart_quantities(store, items)` | **yes** | Absolute quantities (0 removes). Only the cart |
| `get_delivery_info(store)` | no | Chosen delivery time, next free day, fees |
| `get_orders(store)` / `get_order(store, id)` | no | Past orders, for "what we usually buy" |

What it can **never** do: place an order, pay, read cards, choose a delivery time or change the address. That code does not exist, and a test fails if it shows up. See [ADR 0001](docs/adr/0001-the-human-always-pays.md).

## Setup

Needs [uv](https://docs.astral.sh/uv/) (`brew install uv`).

**1. Logins.** One file per store, readable only by you:

```sh
mkdir -p ~/.config/handle-mcp && cd ~/.config/handle-mcp
(umask 077 && touch nemlig.json bilkatogo.json)   # readable only by you from the start
```

Open each file in an editor (not `echo`, which leaves the password in your shell history) and write:

```json
{"email": "you@example.com", "password": "..."}
```

Or use env vars: `HANDLE_NEMLIG_EMAIL`, `HANDLE_NEMLIG_PASSWORD`, `HANDLE_BILKATOGO_EMAIL`, `HANDLE_BILKATOGO_PASSWORD`. Env vars win. The server refuses a login file that others can read.

**2. BilkaToGo:** choose home delivery (not pickup) once on bilkatogo.dk. The server reads the store and fees from your cart.

**3. Add it to your MCP client.** Claude Code:

```sh
claude mcp add handle -- uvx --from git+https://github.com/NJohansen/handle-mcp handle-mcp
```

Claude Desktop (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "handle": {
      "command": "uvx",
      "args": ["--from", "git+https://github.com/NJohansen/handle-mcp", "handle-mcp"]
    }
  }
}
```

Then ask: *"Compare this week's list in nemlig and Bilka, and fill the cheapest cart."* Open the store's website to check the cart, choose a time and pay.

## Security and privacy

- **The human always pays.** Only `set_cart_quantities` writes, and only cart quantities.
- **Personal data stays on your machine.** Every tool answer goes through `privacy.scrub()`. Name, address, phone, email, door code and delivery notes are replaced with `[fjernet af handle-mcp]` before the model sees them.
- **Logins** live in env vars or `~/.config/handle-mcp/` (chmod 600). Never in code, config in git or test fixtures.
- **stdout is JSON-RPC.** Logs go to stderr.
- Use one client at a time per store. Close the store's app or website before an agent changes the cart.
- Be kind to the stores: the server makes few calls, mostly one at a time per store, and retries only on timeouts, network errors, 429 and 5xx.

## Add a store

1. Create `src/handle_mcp/stores/<name>.py` with a class that has every method in `stores.Store`.
2. Raise `StoreError("clear Danish message")` for expected failures, `NotImplementedError` for what the store cannot do.
3. Add one line to `STORES` in `src/handle_mcp/stores/__init__.py`.
4. Tests: real, redacted answers in `tests/fixtures/<name>/`, HTTP mocked with `httpx.MockTransport`.

No change to `server.py` or the tools. More in [CLAUDE.md](CLAUDE.md) and [docs/research.md](docs/research.md).

## Develop

```sh
uv sync
uv run pytest -q                                   # unit tests, no network, no logins
uv run ruff check . && uv run ruff format --check .
HANDLE_LIVE=1 uv run pytest -m live -s             # read-only tests against the real stores
```

## Credits

Built on what these projects found out. Thank you.

- [eisbaw/nemlig_cli](https://github.com/eisbaw/nemlig_cli): the nemlig.com API map (`nemlig_api.md`). MIT.
- [kraenhansen/nemlig-mcp](https://github.com/kraenhansen/nemlig-mcp): nemlig MCP server and the privacy-marker idea. MIT.
- [nickknissen/bilkatogo](https://github.com/nickknissen/bilkatogo): BilkaToGo login, cart and order flow, and the test fixtures in `tests/fixtures/bilkatogo/`. MIT, Copyright (c) 2026 Nick Knissen.
- [stigvoss/bilkatogo-mcp](https://github.com/stigvoss/bilkatogo-mcp): read for facts about BilkaToGo. No code copied.

## License

[MIT](LICENSE)
