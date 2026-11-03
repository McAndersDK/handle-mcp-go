Shapes from eisbaw/nemlig_cli `nemlig_api.md` (MIT), with personal data replaced by
fixed test values (Kunde Eksempel, Testvej 1, 9999 Eksempelby, kunde@example.com). These
files are shapes, not captures, unless a section below says otherwise.

`order_history_flex.json` and `order_details_flex.json` are real captures (2026-11-02) of an
order with flexible delivery. Name, address, notes, coupon, DebitorId and KVHX are replaced
with "X", and the order number is a test value. Product lines, amounts and dates are real.

`search.json` `Recipes`: the fields documented in `nemlig_api.md` (`Id`, `Name`, `Url`,
`TotalTime`, `NumberOfPersons`). Not a live capture yet.

## Offers (live capture, 2026-11-03, logged in, read-only GET)

`offers_page.json`: `GET https://webapi.prod.knl.nemlig.it/productbff/api/web/page?path=/tilbud`,
the data behind nemlig.com/tilbud. `pageContent[]` has `ProductList` blocks with `header.title`
and `products[]` inline (same product shape as the recipe files; `price` and `priceOriginal`
in øre). The offer lists are "Skarp pris", "Ugens priskup", "En gave til dig", "Dagstilbud" and
"Undgå madspild"; the rest ("Frugt og grønt", "Kød", ...) are categories. Trimmed to 5 products
per offer list and 2 of "Frugt og grønt"; one "Mix 2 stk." Priskup product added from the same
capture. `tracking`/`dataLayerTracking` removed, `isFavorite`/`isInShoppingList` false. No user
id needed, unlike the old `/tilbud?GetAsJson=1` + `GetByProductGroupId` (the page URL is queued
by queue-it in busy hours).

## `search_gift.json`

`GET https://webapi.prod.knl.nemlig.it/searchgateway/api/search?query=mums rosinboller&take=10&skip=0&recipeCount=0&timestamp=…&timeslotUtc=…&deliveryZoneId=…`,
trimmed to product 5606321 (Facets, Ads, Recipes emptied). Free gift: `Price` 20.45 but
`Campaign` = `{Type: ProductCampaignFreeProduct, Code: U, CampaignPrice: 0, MaxQuantity: 1,
IntervalEnd: 2026-12-31T22:59:59Z}`, `SubCategory` "Gave fra nemlig.com".

## `delivery_days.json` (live capture, 2026-11-02, logged in, read-only GET)

`GET https://www.nemlig.com/webapi/v2/Delivery/GetDeliveryDays?days=3&showForSubscriptions=false`,
3 days, nothing removed: the answer has no personal data (slot ids are global). `Type` 0 =
Personlig, 1 = Fleksibel. `Availability` 0 = free, 1 = past deadline. Tue 14-21: Personlig 19 kr,
Fleksibel 25 kr. Wed 14-21 Personlig: 0 kr.

## Recipes (live captures, 2026-11-03, read-only GETs)

nemlig's website moved to Angular. Page URLs with `?GetAsJson=1` now give HTML, and in busy
hours they redirect to queue-it. The recipe pages read from `productbff`, with the same Bearer
token as `/webapi/` (`GET /webapi/Token`). `tracking` and `dataLayerTracking` removed,
`isFavorite`/`isInShoppingList` set to false, at most 2 alternatives per ingredient.

| File | Call |
|---|---|
| `recipe_details.json` | `GET https://webapi.prod.knl.nemlig.it/productbff/api/web/meals/recipeDetails/kebabfad-med-fladbrod-98001303` (without `structuredData`) |
| `recipe_products.json` | `GET .../meals/recipeProductDialog?recipeId=kebabfad-med-fladbrod-98001303&servings=4&maxProductsPerIngredient=10`: `includedProducts` to buy, `selfSuppliedProducts` "Vi regner med du har". `price` is øre. `servings` 2-12 changes `item.quantity` |
| `recipes_by_tag.json` | `GET .../meals/allRecipes?skip=0&take=8&categoryPath=aftensmad&filters=familievenlig&filters=budgetvenlig`: `filters[]` is nemlig's tag list (groups of `{path, title}`), `requestedCategory.recipes[]` the hits (tags combine with AND; no `categoryPath` = aftensmad) |
| `meals_page.json` | `GET .../page?path=/maaltider`: "Ugens udvalgte", `pageContent[].recipes[]` |

Also seen in the website's code, not used: `meals/filters/{path}` (one tag, all categories),
`search/recipes`, `meals/purchased` ("Mine opskrifter", 401 without login) and
`POST /webapi/basket/AddRecipeToBasket` (never call it: we fill the cart with
`set_cart_quantities` after the human says yes).
