# Build your own skill on top

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

## Buy one store's recipe in another store

Turn the `items` of `get_recipe` into `compare_basket` lines for the other store:

- Use `items[].ingredient` as the name. Skip `have_at_home` items (salt, oil) unless the human wants them.
- Find the matching line in `ingredients` (the text is written for `ingredients_persons`). Take the amount only when the unit is g, kg, ml, dl, l or stk, scaled to `persons`: `"500 g hakket svin"` for 4 persons becomes `"hakket svin 750 g"` for 6. Otherwise give the name alone (`"hvidløg"`, not `"2 fed hvidløg"`), and let the human say how much.
- Fractions (`"1/2 dl"`, `"½ dl"`, `"1 1/2 dl"`) are fine with a unit the matcher knows. Do not pass fed, bdt., tsk. or spsk.: they are not units, so the matcher does not read them as amounts. Convert them yourself or leave the amount out.
- Read `get_cart` for the other store before `set_cart_quantities`: quantities are absolute, so add to what is already there.
