# REMA 1000 fixtures

Real answers from REMA 1000's API, captured on 2026-11-07 with GET requests only. Nothing was
written to REMA. Personal and location data is masked. The adapter's tests read these files
(`internal/store/rema1000`). If REMA changes its API, a stale fixture is easy to spot from this date.

## Logged in (masked)

| File | Call | Masking |
|---|---|---|
| `stores.json` | `v3/stores` (3 stores, paged) | Fictional stores: names, addresses, cities, phone, opening hours, coordinates and ids replaced. |
| `suggested_stores.json` | `v3/users/{id}/suggested-stores` | Empty list; the user id in the paging links is replaced. |
| `time_slots.json` | `v3/stores/{id}/time-slots` | The store is fictional. `data` is an object of day -> pickup times (UTC), each with `from`, `to`, `is_available`. |
| `lists.json` | `v3/users/{id}/shopping-lists` | List ids, names and the user id replaced. |
| `list_items.json` | `v2/shoppinglists/{id}?with=items` | Ids and the list code replaced; cut to 2 items. Never fetched with `with=users`, which holds names and emails. |

`search_milk.json` (`search/products?query=mælk&per_page=10`) is public product data, the same logged in and out; it
holds nothing personal.

## Public, logged out (not masked, nothing personal in them)

| File | Call |
|---|---|
| `offers.json` | `search/products?filter[labels]=9001&sort=department&per_page=6`: the weekly leaflet (label Avisvare). |
| `recipes_search.json` | `search/recipes?query=kylling&per_page=3`, cut to 2 recipes. |
| `recipes_listing.json` | `rema1000dk/entities?filter[type]=recipe&per_page=3&sort=-created_at`, cut to 2 recipes. |
| `recipe_by_slug.json` | `rema1000dk/entities?filter[type]=recipe&filter[slug]=kylling-i-tarteletter`: one whole recipe, with the product REMA picked for each ingredient. |
| `recipe_tags.json` | `rema1000dk/entities?filter[type]=recipeTag&per_page=100`: the 30 recipe tags. |

The shapes the adapter relies on: a recipe's `fields.customForm.ingredientGroups[].ingredients[]`
has `ingredient`, `amountOfContent`, `unitOfContent`, `isExcludedFromShoppingList` and
`digitalProduct` (`amount` = packs at the recipe's own persons, `is_basic_ingredient`,
`active_product`, which can be missing). A price is a list of periods, the current one first.
