# Tools

| Tool | Writes? | What |
|---|---|---|
| `search_products(query, stores?)` | no | Price, price per kg/l, offers, EAN when known |
| `search_recipes(query, stores?)` | no | The store's own recipes: name and link (nemlig only). `get_recipe` gives time, persons and products |
| `list_recipes(stores?, tags?, category?, limit?)` | no | This week's featured recipes (nemlig "Ugens udvalgte"), or with `tags` all recipes that have every tag, e.g. `["Familievenlig", "Budgetvenlig"]` or `["Under 30 min", "Kylling"]`. `category`: aftensmad (default), frokost, madpakke, ... An unknown tag answers with nemlig's list |
| `get_recipe(store, recipe, persons?)` | no | The products behind the store's "Se varer og læg i kurv": per ingredient the product, packs for `persons`, price and alternatives. `have_at_home` items (salt, oil) are left out of `total`. Fill the cart with `set_cart_quantities` after a yes |
| `list_offers(stores?, limit?)` | no | This week's offers, also items you would not search for: nemlig Skarp pris, Priskup, Dagstilbud, Undgå madspild, gifts; BilkaToGo offers. Free gifts have price 0 and a max per order |
| `compare_basket(items, stores?)` | no | Prices a shopping list in each store: up to 5 candidates per item with facts and flags, fees, minimum order, unsure and missing items marked, cheapest store |
| `get_cart(store)` | no | Cart lines, fees, total, minimum order |
| `set_cart_quantities(store, items)` | **yes** | Absolute quantities (0 removes). Only the cart |
| `get_delivery_info(store)` | no | Chosen delivery time, next free day, fees |
| `list_delivery_slots(stores?, days?)` | no | Free delivery times with the fee per slot, the 3 cheapest first |
| `get_orders(store)` / `get_order(store, id)` | no | Past orders, for "what we usually buy" |

What it can **never** do: place an order, pay, read cards, choose a delivery time or change the address. That code does not exist, and a test fails if it shows up. See [ADR 0001](adr/0001-the-human-always-pays.md).


Fields in each `compare_basket` candidate: [skills.md](skills.md).
