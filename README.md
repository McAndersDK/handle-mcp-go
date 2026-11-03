<div align="center">

# 🧺 handle-mcp

<a href="https://modelcontextprotocol.io"><img src="docs/badges/mcp.svg" alt="made for MCP" height="26"></a>
<img src="docs/badges/stores.svg" alt="stores: nemlig, BilkaToGo" height="26">
<img src="docs/badges/python.svg" alt="built with Python 3.12+" height="26">
<a href="LICENSE"><img src="docs/badges/license.svg" alt="license MIT" height="26"></a>

<img src="docs/hero.svg" alt="Give AI a budget, fill it cheaply, you pay and they deliver, unpack the week, enjoy dinner" width="800">

**Your AI agent plans the week's meals on a budget, finds the cheapest cart and fills it. You approve and pay.**

</div>

Danish grocery stores with home delivery, as an MCP server: [nemlig.com](https://www.nemlig.com) and [BilkaToGo](https://www.bilkatogo.dk).

**It can never order or pay. You do.** That code does not exist, and a test fails if it shows up ([why](docs/adr/0001-the-human-always-pays.md)).

## How it works

1. **Plan.** Give your agent a budget. It plans meals around this week's offers.
2. **Compare.** It prices the list in every store, fees included.
3. **Fill.** It fills the cheapest cart.
4. **Pay.** You check the cart on the store's website, pick a time and pay.

Taste, budget and meal rules live in your own prompt or skill. The server finds and counts. [Build your own skill](docs/skills.md).

## Try it

```sh
uvx --from git+https://github.com/NJohansen/handle-mcp handle-mcp login nemlig
claude mcp add handle -- uvx --from git+https://github.com/NJohansen/handle-mcp handle-mcp
```

Then ask:

- *"Plan 5 dinners for 4 within 900 kr, built on this week's offers. Fill the cheapest cart."*
- *"What is on offer this week? Any free gifts?"*
- *"Which delivery times are cheapest tomorrow?"*

BilkaToGo, Claude Desktop and other logins: [setup](docs/setup.md).

## Tools

| Tool | Writes? | What |
|---|---|---|
| `search_products` | no | Price, price per kg/l, offers |
| `search_recipes` | no | The store's recipes (nemlig) |
| `list_recipes` | no | Featured recipes, or by tag: Familievenlig, Budgetvenlig, Under 30 min ... (nemlig) |
| `get_recipe` | no | The products to buy for a recipe, for any number of persons |
| `list_offers` | no | This week's offers and free gifts |
| `compare_basket` | no | Prices a list in every store, fees included, cheapest first |
| `get_cart` | no | Cart lines, fees, total |
| `set_cart_quantities` | **yes** | Sets quantities in the cart. Nothing else |
| `get_delivery_info` | no | Chosen time, fees |
| `list_delivery_slots` | no | Free times, cheapest first |
| `get_orders` / `get_order` | no | What you usually buy |

Full arguments and fields: [docs/tools.md](docs/tools.md).

## More

[Setup](docs/setup.md) · [Skills](docs/skills.md) · [Security](docs/security.md) · [Troubleshooting](docs/troubleshooting.md) · [Roadmap](docs/roadmap.md) · [Contributing](CONTRIBUTING.md) · [Credits](CREDITS.md)

Unofficial. Uses the stores' private website APIs, so it can break any day. Not affiliated with nemlig.com or Salling Group.

[MIT](LICENSE)
