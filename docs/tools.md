# Tools

| Tool | Writes? | Butikker | What |
|---|---|---|---|
| `search_products(query, stores?)` | no | nemlig, BilkaToGo, REMA 1000 | Price, price per kg/l, offers, EAN when known. All stores need a login (REMA 1000: the search itself is public, but every REMA call needs a login and a chosen pickup store) |
| `search_recipes(query, stores?)` | no | nemlig, REMA 1000 | The store's own recipes: name and link. `get_recipe` gives time, persons and products. BilkaToGo has no recipe search: use `list_recipes` with `category`. REMA 1000 searches its own recipes; the id is the slug in the recipe's web address |
| `list_recipes(stores?, tags?, category?, limit?)` | no | nemlig, BilkaToGo, REMA 1000 | This week's featured recipes (nemlig "Ugens udvalgte"), or with `tags` all recipes that have every tag, e.g. `["Familievenlig", "Budgetvenlig"]` or `["Under 30 min", "Kylling"]`. `category`: aftensmad (default), frokost, madpakke, ... An unknown tag answers with nemlig's list. BilkaToGo has no tags: without `category` it is this week's "Madplan" (7 dinners, Monday first), with `category` every recipe in that part of bilkatogo.dk/inspiration/opskrifter (`aftensmad`, `aftensmad/pastaretter`, `boern-i-koekkenet` ...), mixed across sub-categories. An unknown category answers with BilkaToGo's list. REMA 1000: without `tags` its most popular recipes; `tags` are its recipe tags (Aftensmad, Kødfri, Mad for børn, Nem hverdagsmad, Jul, ...), all of them must match; `category` counts as one more tag; an unknown tag answers with the list of known ones |
| `get_recipe(store, recipe, persons?)` | no | nemlig, BilkaToGo, REMA 1000 | The products behind the store's "Se varer og læg i kurv": per ingredient the product, packs for `persons`, price and alternatives. `have_at_home` items (salt, oil) are left out of `total`. Also the dish's photo (`image`), `description`, `method` (sections with steps) and `tip`. The `ingredients` text is for `ingredients_persons`, not `persons`. Fill the cart with `add_recipe_to_cart` after a yes. BilkaToGo: one pack of each product on the page's "Det skal du bruge" (no amounts, no alternatives, no `have_at_home`); `persons` is not used, the recipe's own persons come back (`null` when written for pieces, e.g. "16 stk."); `missing` lists product ids not sold at your address; prices need a login. REMA 1000: the product REMA picked for each ingredient; the packs follow `persons` (rounded up, from the packs REMA gives at the recipe's own persons); basic items (salt, flour, onions ...) are `have_at_home`; `missing` lists ingredient names without a product (not ids); no alternatives |
| `list_offers(stores?, limit?)` | no | nemlig, BilkaToGo, REMA 1000 | This week's offers, also items you would not search for: nemlig Skarp pris, Priskup, Dagstilbud, Undgå madspild, gifts; BilkaToGo offers. Free gifts have price 0 and a max per order. nemlig needs no login (with one, the offers fit your delivery time); BilkaToGo does, because the store follows your address. REMA 1000: the weekly leaflet (label Avisvare, the same list as rema1000.dk/avisvarer), sorted by department, with the last day of the offer |
| `compare_basket(items, stores?)` | no | nemlig, BilkaToGo, REMA 1000 | Prices a shopping list in each store: up to 5 candidates per item with facts and flags, fees, minimum order, unsure and missing items marked, cheapest store. Each store has `delivery_mode` and `pickup_store`; a pickup store gets a note, and when one store delivers and another is pickup, the summary names the cheapest of each (the trip to the store is not counted) |
| `get_cart(store?)` | no | nemlig, BilkaToGo, REMA 1000 | Cart lines, fees, total, minimum order, `delivery_mode` and `pickup_store`. Without `store`: every store's cart, keyed by store name. REMA 1000: the cart is today's shopping list "handle-mcp YYYY-MM-DD" (lines bought in the app are left out); no list for today is an empty cart. Deposit is the fee `pant` |
| `set_cart_quantities(store, items)` | **yes** | nemlig, BilkaToGo, REMA 1000 | Absolute quantities (0 removes). Read `get_cart` first and add to what is there. Only the cart. REMA 1000: writes only to today's list "handle-mcp YYYY-MM-DD", which it creates if missing (never as the primary list); every amount is read back. See [ADR 0008](adr/0008-rema-basket-is-a-dated-shopping-list.md) |
| `add_recipe_to_cart(store, recipe, persons?, skip?, swap?, include?)` | **yes** | nemlig, BilkaToGo, REMA 1000 | A recipe's products from `get_recipe` into the cart, added to what is there (twice adds twice). Ask first. `have_at_home` items only with `include`; `skip` and `swap` (to an alternative's id) by ingredient or product id. Answers before/added/after per product and what was skipped (sold out, have at home, left out) |
| `get_delivery_info(store)` | no | nemlig, BilkaToGo, REMA 1000 | Chosen delivery or pickup time, next free day, fees, `delivery_mode` and `pickup_store` (in both `delivery` and `terms`). REMA 1000: always `"pickup"` in the first of your chosen stores that has Køb & Hent (the note says when a preferred store is not active yet); no time is chosen (it is chosen and paid on rema1000.dk, the note says so); fee and minimum order are `null` because REMA does not give them |
| `list_delivery_slots(stores?, days?)` | no | nemlig, BilkaToGo, REMA 1000 | Free delivery or pickup times with the fee per slot, in time order, and the 3 cheapest first. A BilkaToGo pickup cart gives slots with kind "Afhentning", `pickup_store` and the fee (`"0.00"` when free). REMA 1000: pickup times in the active chosen store only, kind "Afhentning", fee `null` with a note, `available` false for full times |
| `get_orders(store)` / `get_order(store, id)` | no | nemlig, BilkaToGo | Past orders, for "what we usually buy". REMA 1000 says "kan ikke det endnu": no order endpoint is known |

Money is a string in kroner with two decimals (`"12.95"`, `"39.00"`) in every store. Dates are ISO. A fact the store does not give is `null`, never left out.

`delivery_mode` is `"home"`, `"pickup"` or `null` (the store does not say). `pickup_store` is the store's name, only for pickup. nemlig is always `"home"`; REMA 1000 is always `"pickup"` (it has no home delivery); BilkaToGo says `"pickup"` for a pickup cart and `null` when it does not show which. The tools only read the mode: the human chooses home delivery or pickup, a pickup store and a time on the store's website. See [ADR 0006](adr/0006-pickup-is-a-delivery-mode.md).

Every product from `search_products` and `list_offers` has the same fields. Two of them are often `null`:

| Field | nemlig | BilkaToGo |
|---|---|---|
| `before_price` (the price without the offer) | when nemlig shows a crossed-out price; a "Skarp pris" without one has `null` | when the offer has a normal price |
| `offer_until` (last day of the offer, ISO date) | from `search_products`; nemlig's offer lists (`list_offers`) do not say, so `null` | always `null`: BilkaToGo does not say |

`compare_basket` candidates list only the facts that are known, to stay short.

What it can **never** do: place an order, pay, read cards, choose a delivery time, a pickup store or home delivery or pickup, or change the address. That code does not exist, and a test fails if it shows up. See [ADR 0001](adr/0001-the-human-always-pays.md).


Fields in each `compare_basket` candidate: [skills.md](skills.md).

## REMA 1000

REMA 1000 works only while at least one of your chosen stores has Køb & Hent. `handle-mcp login rema1000` logs in, searches REMA's store list for a postcode or name and asks you to pick one or more stores by number, the preferred first; the choice is saved on your machine with the session and is never sent to REMA. It is checked at the first call and again once an hour, so REMA 1000 switches on by itself when a chosen store gets Køb & Hent, and off when it is lost. The first active store in your order is the pickup store. Otherwise every REMA call answers with how to fix it. The cart is a shopping list called "handle-mcp YYYY-MM-DD"; you choose the pickup time and pay on rema1000.dk. See [ADR 0008](adr/0008-rema-basket-is-a-dated-shopping-list.md).

## Recipes across stores

A recipe from one store can be bought in another. `add_recipe_to_cart` cannot do it: it reads the recipe again from the store you name and accepts only that store's own alternatives. Use the existing tools instead:

1. `get_recipe(storeA, recipe)` for the ingredients.
2. `compare_basket(lines, stores=[storeB])` with one line per ingredient: names with amounts, never storeA's product ids, which work only in storeA.
3. The human picks a candidate per line.
4. `get_cart(storeB)`, because `set_cart_quantities` is absolute and would overwrite what is already there.
5. `set_cart_quantities(storeB, ids)` with the old counts plus the new ones.

How to turn a recipe into `compare_basket` lines: [skills.md](skills.md).
