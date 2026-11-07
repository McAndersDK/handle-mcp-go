# Tools

| Tool | Writes? | Butikker | What |
|---|---|---|---|
| `search_products(query, stores?)` | no | nemlig, BilkaToGo | Price, price per kg/l, offers, EAN when known. Both stores need a login |
| `search_recipes(query, stores?)` | no | nemlig | The store's own recipes: name and link. `get_recipe` gives time, persons and products. BilkaToGo has no recipe search: use `list_recipes` with `category` |
| `list_recipes(stores?, tags?, category?, limit?)` | no | nemlig, BilkaToGo | This week's featured recipes (nemlig "Ugens udvalgte"), or with `tags` all recipes that have every tag, e.g. `["Familievenlig", "Budgetvenlig"]` or `["Under 30 min", "Kylling"]`. `category`: aftensmad (default), frokost, madpakke, ... An unknown tag answers with nemlig's list. BilkaToGo has no tags: without `category` it is this week's "Madplan" (7 dinners, Monday first), with `category` every recipe in that part of bilkatogo.dk/inspiration/opskrifter (`aftensmad`, `aftensmad/pastaretter`, `boern-i-koekkenet` ...), mixed across sub-categories. An unknown category answers with BilkaToGo's list |
| `get_recipe(store, recipe, persons?)` | no | nemlig, BilkaToGo | The products behind the store's "Se varer og læg i kurv": per ingredient the product, packs for `persons`, price and alternatives. `have_at_home` items (salt, oil) are left out of `total`. Also the dish's photo (`image`), `description`, `method` (sections with steps) and `tip`. The `ingredients` text is for `ingredients_persons`, not `persons`. Fill the cart with `add_recipe_to_cart` after a yes. BilkaToGo: one pack of each product on the page's "Det skal du bruge" (no amounts, no alternatives, no `have_at_home`); `persons` is not used, the recipe's own persons come back (`null` when written for pieces, e.g. "16 stk."); `missing` lists product ids not sold at your address; prices need a login |
| `list_offers(stores?, limit?)` | no | nemlig, BilkaToGo | This week's offers, also items you would not search for: nemlig Skarp pris, Priskup, Dagstilbud, Undgå madspild, gifts; BilkaToGo offers. Free gifts have price 0 and a max per order. nemlig needs no login (with one, the offers fit your delivery time); BilkaToGo does, because the store follows your address |
| `compare_basket(items, stores?)` | no | nemlig, BilkaToGo | Prices a shopping list in each store: up to 5 candidates per item with facts and flags, fees, minimum order, unsure and missing items marked, cheapest store. Each store has `delivery_mode` and `pickup_store`; a pickup store gets a note, and when one store delivers and another is pickup, the summary names the cheapest of each (the trip to the store is not counted) |
| `get_cart(store?)` | no | nemlig, BilkaToGo | Cart lines, fees, total, minimum order, `delivery_mode` and `pickup_store`. Without `store`: every store's cart, keyed by store name |
| `set_cart_quantities(store, items)` | **yes** | nemlig, BilkaToGo | Absolute quantities (0 removes). Only the cart |
| `add_recipe_to_cart(store, recipe, persons?, skip?, swap?, include?)` | **yes** | nemlig, BilkaToGo | A recipe's products from `get_recipe` into the cart, added to what is there (twice adds twice). Ask first. `have_at_home` items only with `include`; `skip` and `swap` (to an alternative's id) by ingredient or product id. Answers before/added/after per product and what was skipped (sold out, have at home, left out) |
| `get_delivery_info(store)` | no | nemlig, BilkaToGo | Chosen delivery or pickup time, next free day, fees, `delivery_mode` and `pickup_store` (in both `delivery` and `terms`) |
| `list_delivery_slots(stores?, days?)` | no | nemlig, BilkaToGo | Free delivery or pickup times with the fee per slot, in time order, and the 3 cheapest first. A BilkaToGo pickup cart gives slots with kind "Afhentning", `pickup_store` and the fee (`"0.00"` when free) |
| `get_orders(store)` / `get_order(store, id)` | no | nemlig, BilkaToGo | Past orders, for "what we usually buy" |

Money is a string in kroner with two decimals (`"12.95"`, `"39.00"`) in every store. Dates are ISO. A fact the store does not give is `null`, never left out.

`delivery_mode` is `"home"`, `"pickup"` or `null` (the store does not say). `pickup_store` is the store's name, only for pickup. nemlig is always `"home"`; BilkaToGo says `"pickup"` for a pickup cart and `null` when it does not show which. The tools only read the mode: the human chooses home delivery or pickup, a pickup store and a time on the store's website. See [ADR 0006](adr/0006-pickup-is-a-delivery-mode.md).

Every product from `search_products` and `list_offers` has the same fields. Two of them are often `null`:

| Field | nemlig | BilkaToGo |
|---|---|---|
| `before_price` (the price without the offer) | when nemlig shows a crossed-out price; a "Skarp pris" without one has `null` | when the offer has a normal price |
| `offer_until` (last day of the offer, ISO date) | from `search_products`; nemlig's offer lists (`list_offers`) do not say, so `null` | always `null`: BilkaToGo does not say |

`compare_basket` candidates list only the facts that are known, to stay short.

What it can **never** do: place an order, pay, read cards, choose a delivery time, a pickup store or home delivery or pickup, or change the address. That code does not exist, and a test fails if it shows up. See [ADR 0001](adr/0001-the-human-always-pays.md).


Fields in each `compare_basket` candidate: [skills.md](skills.md).
