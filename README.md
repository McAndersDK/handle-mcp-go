<div align="center">

# 🧺 handle-mcp

<a href="https://modelcontextprotocol.io"><img src="docs/badges/mcp.svg" alt="made for MCP" height="26"></a>
<img src="docs/badges/stores.svg" alt="stores: nemlig, BilkaToGo" height="26">
<img src="docs/badges/python.svg" alt="built with Python 3.12+" height="26">
<a href="LICENSE"><img src="docs/badges/license.svg" alt="license MIT" height="26"></a>

<img src="docs/hero.svg" alt="Pixels move from an AI sparkle to a shopping basket, a delivery van, a box and a meal" width="600">

**Your AI agent plans the week's meals on a budget, finds the cheapest cart and fills it. You approve and pay.**

</div>

A local MCP server for Danish grocery stores with home delivery. An AI agent can search products, read this week's offers, compare a shopping list across stores and fill the cart. **You always approve and pay yourself**, on the store's own website.

Stores in v1: [nemlig.com](https://www.nemlig.com) and [BilkaToGo](https://www.bilkatogo.dk) (home delivery).

> [!WARNING]
> Unofficial. The stores have no public API. This uses the same private APIs as their websites, and it can break at any time. Not affiliated with nemlig.com or Salling Group.

## The idea

Weekly food shopping is a lot of small decisions: what to cook, what is on offer, which store is cheapest, and does it fit the budget. handle-mcp gives your agent the store data, so it can do the whole loop:

1. **Plan.** The agent plans the week's meals from your budget, your taste and this week's offers (`list_offers`, `search_recipes` (nemlig), `get_orders`).
2. **Compare.** It turns the plan into a shopping list and prices it in every store, fees and minimum order included (`compare_basket`). It picks a product per item and asks you when it is unsure.
3. **Fill.** It fills the cheapest store's cart (`set_cart_quantities`, the only write).
4. **You pay.** You open the store's website, check the cart, choose a delivery time and pay.

Your budget, taste and meal rules live in your own prompt or skill, not in the server. See [Build your own skill on top](#build-your-own-skill-on-top).

## What you can ask

**Meal plan on a budget**
- *"Plan 5 dinners for 4 people within 900 kr, built on this week's offers. Then compare the list and fill the cheapest cart."*
- *"We have rice, pasta and chickpeas at home. Plan around them."*

**Compare and save**
- *"Here is this week's list. Which store is cheapest, fees included?"*
- *"Price per kg for minced meat in both stores?"*

**Offers**
- *"What is on offer this week? Any free gifts?"*

**Cart and delivery**
- *"Fill the nemlig cart with the cheapest basket."*
- *"Which delivery times are cheapest tomorrow?"*

**Habits**
- *"What did we buy in our last orders?"*
- *"Find a lasagne recipe from the store."* (nemlig)

## What it can do

| Tool | Writes? | What |
|---|---|---|
| `search_products(query, stores?)` | no | Price, price per kg/l, offers, EAN when known |
| `search_recipes(query, stores?)` | no | The store's own recipes: name, link, time, persons (nemlig only). Ingredients are on the recipe page |
| `list_offers(stores?, limit?)` | no | This week's offers, also items you would not search for: nemlig Skarp pris, Priskup, Dagstilbud, Undgå madspild, gifts; BilkaToGo offers. Free gifts have price 0 and a max per order |
| `compare_basket(items, stores?)` | no | Prices a shopping list in each store: up to 5 candidates per item with facts and flags, fees, minimum order, unsure and missing items marked, cheapest store |
| `get_cart(store)` | no | Cart lines, fees, total, minimum order |
| `set_cart_quantities(store, items)` | **yes** | Absolute quantities (0 removes). Only the cart |
| `get_delivery_info(store)` | no | Chosen delivery time, next free day, fees |
| `list_delivery_slots(stores?, days?)` | no | Free delivery times with the fee per slot, the 3 cheapest first |
| `get_orders(store)` / `get_order(store, id)` | no | Past orders, for "what we usually buy" |

What it can **never** do: place an order, pay, read cards, choose a delivery time or change the address. That code does not exist, and a test fails if it shows up. See [ADR 0001](docs/adr/0001-the-human-always-pays.md).

## Setup

Needs [uv](https://docs.astral.sh/uv/) (`brew install uv`).

**1. Logins.** Save each store's login in your OS keychain (macOS Keychain, Windows Credential Locker or Linux Secret Service). It asks for email and password, checks them against the store, and saves them encrypted:

```sh
uvx --from git+https://github.com/NJohansen/handle-mcp handle-mcp login nemlig
uvx --from git+https://github.com/NJohansen/handle-mcp handle-mcp login bilkatogo
```

`handle-mcp logout <store>` removes it again. From a clone, use `uv run handle-mcp login nemlig`.

Other ways, checked in this order:
1. Env vars `HANDLE_<STORE>_EMAIL` and `HANDLE_<STORE>_PASSWORD` (e.g. `HANDLE_NEMLIG_EMAIL`).
2. The keychain (above). Recommended.
3. A file `~/.config/handle-mcp/<store>.json` with `{"email": "...", "password": "..."}`. Plain text on disk, so the server refuses it unless only you can read it (`chmod 600`). Create it with `umask 077` and an editor, not `echo`, which leaves the password in your shell history.

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

Then ask: *"Compare this week's list in nemlig and Bilka, and fill the cheapest cart."* Open the store's website to check the cart, choose a time and pay. More prompts: [What you can ask](#what-you-can-ask).

## Build your own skill on top

The server finds and counts. Your skill or agent picks. Taste, favourites, budget and øko rules belong in your skill, never in this server.

`compare_basket` gives each list line up to 5 `candidates`. The first has `suggested: true`: the server's guess by word rules, nothing more. When the search found an øko product, one candidate is øko.

| Field | Meaning |
|---|---|
| `id` | Product id. Use it in `set_cart_quantities` |
| `name`, `brand`, `size`, `category` | As the store shows them, e.g. category `Kolonial > Konserves` |
| `quantity` | Packs needed for the amount on the list |
| `line_total` | Cost for that amount, offers included. Compare on this |
| `price`, `unit_price`, `unit` | Price per pack, and per kg, l or stk |
| `offer_text` | The store's offer, e.g. `2 for 30` |
| `name_match` | `full`, `partial` or `none` (a guess: no word on the list matched) |
| `size_fits` | `false`: the packs give too much or too little. Left out when sizes cannot be compared |
| `prepared` | Cold cuts, smoked, breaded |
| `processed` | `frost` or `konserves` |
| `blend` | A mix, e.g. minced meat with vegetables |
| `oeko` | Organic |
| `note` | Why the line is unsure, in Danish |

Flags that are false and facts that are unknown are left out, to keep the answer small. `sure: false` on a line means: look, or ask the user. When no candidate fits, call `search_products` with other words.

## Security and privacy

- **The human always pays.** Only `set_cart_quantities` writes, and only cart quantities.
- **Personal data stays on your machine.** Every tool answer goes through `privacy.scrub()`. Name, address, phone, email, door code and delivery notes are replaced with `[fjernet af handle-mcp]` before the model sees them.
- **Logins** live in your OS keychain, env vars or `~/.config/handle-mcp/` (chmod 600). Never in code, config in git or test fixtures. `handle-mcp login` never shows or logs the password.
- **stdout is JSON-RPC.** Logs go to stderr. httpx request logging is off, because some store URLs contain account ids.
- **Store text reaches the model.** Product names, offer texts and cart messages come from the stores and land in the model's context. A store (or a seller) could put instructions in them. The server cannot order or pay, so the damage is limited to a wrong cart. Check the cart before you pay.
- **Versions.** `git+https://...` installs the newest `main`. Pin a tag when one exists: `git+https://github.com/NJohansen/handle-mcp@v0.1.0`.
- Use one client at a time per store. Close the store's app or website before an agent changes the cart.
- Be kind to the stores: the server makes few calls, mostly one at a time per store, and retries only on timeouts, network errors, 429 and 5xx.

## Troubleshooting

Errors from a store come back to the agent as one short Danish line, e.g. `nemlig: ...`. One store failing never stops the others.

| Message | Fix |
|---|---|
| `Intet login til <store>` | Run `handle-mcp login <store>` in a terminal. |
| `afviser login. Tjek email og kodeord.` | Wrong email or password. Run `handle-mcp login <store>` again. |
| `nemlig.com har kø lige nu (queue-it)` | nemlig has a queue. Try again in a few minutes. |
| `Kurven er sat til afhentning` (BilkaToGo) | Choose home delivery (not pickup) once on bilkatogo.dk. |
| `uventet fejl (...)` | The store's API may have changed. The log on stderr has the details. Please open an issue. |

## Roadmap

- **v1 (done):** nemlig.com + BilkaToGo. Search, cart, delivery info, orders, offers, recipes, whole-basket comparison.
- **v2:** better matching across stores (EAN + AI), favourites.
- **v3:** meal plans: recipes, ratings and "what we usually buy".
- **v4:** more stores when they offer home delivery. Maybe a hosted version.

## Add a store

1. Create `src/handle_mcp/stores/<name>.py` with a class that has every method in `stores.Store`.
2. Raise `StoreError("clear Danish message")` for expected failures, `NotImplementedError` for what the store cannot do.
3. Add one line to `STORES` in `src/handle_mcp/stores/__init__.py`.
4. Tests: real, redacted answers in `tests/fixtures/<name>/`, HTTP mocked with `httpx.MockTransport`.

No change to `server.py` or the tools. More in [CONTRIBUTING.md](CONTRIBUTING.md), [CLAUDE.md](CLAUDE.md) and [docs/research.md](docs/research.md).

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
