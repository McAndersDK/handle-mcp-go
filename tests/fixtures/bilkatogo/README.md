Real, redacted BilkaToGo answers from nickknissen/bilkatogo
(https://github.com/nickknissen/bilkatogo, MIT License, Copyright (c) 2026 Nick Nissen).

`cart_logged_in.json` is a pickup cart (Bilka Vejle). `cart_home.json` is the same cart
changed by hand to look like home delivery. It is not a real capture. A live test on
2026-11-01 confirmed the fields it uses (fees, minimum order, address marker).

`algolia_offers.json`: live capture 2026-11-02 (public search-only key, read-only). One query:

    POST https://f9vbjlr1bk-dsn.algolia.net/1/indexes/*/queries
    {"requests": [{"indexName": "prod_BILKATOGO_PRODUCTS", "params":
      "query=&hitsPerPage=10&filters=isInOffer:1653 AND isInAssortmentIn:1653 AND nonsearchable:false
       &attributesToRetrieve=[name,brand,gtin,netcontent,productType,price,isInCurrentLeaflet,cpOffer,multipromos,storeData.1653]"}]}

`isInOffer` is a list of store ids (not `true`), so filter `isInOffer:<sid>`. 1653 is a public
store id. Offer fields are in `storeData.<sid>`: `offerDescription` ("Skarp pris",
"Mix 10 for 25,00", "Se Her"), `price` / `unitsOfMeasureOfferPrice` in øre, `multiPromoPrice` +
`offerCount` for mix offers, `offerMax` / `offerMaxDescription`, `offerFrom` / `offerUntil`
(empty in this capture), `beforePrice` (0 in this capture).

## `available_delivery_dates.json` (live capture, 2026-11-02, logged in, read-only GET)

`GET https://api.bilkatogo.dk/api/shop/v3/AvailableDeliveryDates?u=w`, trimmed to 3 days. `uid`,
`eid` and `tid` (personal/session ids) are replaced with "X". Home delivery, night slot only.
Mon is closed (past deadline), Tue is sold out (`closed_text` "udsolgt", no intervals), Wed is open.

## `recipes/` (live capture, 2026-11-06, no login, read-only GET)

`GET https://www.bilkatogo.dk/_nuxt/static/1793920097/inspiration/opskrifter/<path>/payload.js`
for `""` (opskrifter), `madplaner` (week 46), `aftensmad`, `aftensmad/danske-retter`,
`aftensmad/danske-retter/karbonader` and `aftensmad/pastaretter/carbonara`, saved as they came.
`unknown-page.html` is what the site answers for a page it does not know
(`/inspiration/opskrifter/nope/`): HTTP 200, the website's HTML with the build id.
No personal data: the pages are public. The only name is the recipes' author.
In `test_bilkatogo_recipes.py` the Algolia hits for the recipe's products are
`algolia_products.json` hits with `objectID` changed by hand to karbonader's item numbers.
