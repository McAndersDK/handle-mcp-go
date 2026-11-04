# Tools

| Tool | Writes? | Butikker | What |
|---|---|---|---|
| `search_products(query, stores?)` | no | nemlig, BilkaToGo | Price, price per kg/l, offers, EAN when known. Both stores need a login |
| `search_recipes(query, stores?)` | no | nemlig | The store's own recipes: name and link. `get_recipe` gives time, persons and products |
| `list_recipes(stores?, tags?, category?, limit?)` | no | nemlig | This week's featured recipes (nemlig "Ugens udvalgte"), or with `tags` all recipes that have every tag, e.g. `["Familievenlig", "Budgetvenlig"]` or `["Under 30 min", "Kylling"]`. `category`: aftensmad (default), frokost, madpakke, ... An unknown tag answers with nemlig's list |
| `get_recipe(store, recipe, persons?)` | no | nemlig | The products behind the store's "Se varer og læg i kurv": per ingredient the product, packs for `persons`, price and alternatives. `have_at_home` items (salt, oil) are left out of `total`. Fill the cart with `add_recipe_to_cart` after a yes |
| `list_offers(stores?, limit?)` | no | nemlig, BilkaToGo | This week's offers, also items you would not search for: nemlig Skarp pris, Priskup, Dagstilbud, Undgå madspild, gifts; BilkaToGo offers. Free gifts have price 0 and a max per order. nemlig needs no login (with one, the offers fit your delivery time); BilkaToGo does, because the store follows your address |
| `compare_basket(items, stores?)` | no | nemlig, BilkaToGo | Prices a shopping list in each store: up to 5 candidates per item with facts and flags, fees, minimum order, unsure and missing items marked, cheapest store |
| `get_cart(store)` | no | nemlig, BilkaToGo | Cart lines, fees, total, minimum order |
| `set_cart_quantities(store, items)` | **yes** | nemlig, BilkaToGo | Absolute quantities (0 removes). Only the cart |
| `add_recipe_to_cart(store, recipe, persons?, skip?, swap?, include?)` | **yes** | nemlig | A recipe's products from `get_recipe` into the cart, added to what is there (twice adds twice). Ask first. `have_at_home` items only with `include`; `skip` and `swap` (to an alternative's id) by ingredient or product id. Answers before/added/after per product and what was skipped (sold out, have at home, left out) |
| `get_delivery_info(store)` | no | nemlig, BilkaToGo | Chosen delivery time, next free day, fees |
| `list_delivery_slots(stores?, days?)` | no | nemlig, BilkaToGo | Free delivery times with the fee per slot, the 3 cheapest first |
| `get_orders(store)` / `get_order(store, id)` | no | nemlig, BilkaToGo | Past orders, for "what we usually buy" |

What it can **never** do: place an order, pay, read cards, choose a delivery time or change the address. That code does not exist, and a test fails if it shows up. See [ADR 0001](adr/0001-the-human-always-pays.md).


Fields in each `compare_basket` candidate: [skills.md](skills.md).
