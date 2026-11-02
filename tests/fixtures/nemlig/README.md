Shapes from eisbaw/nemlig_cli `nemlig_api.md` (MIT), with personal data replaced by
fixed test values (Kunde Eksempel, Testvej 1, 9999 Eksempelby, kunde@example.com).
Replace with real, redacted captures when they exist.

`search.json` `Recipes`: the fields documented in `nemlig_api.md` (`Id`, `Name`, `Url`,
`TotalTime`, `NumberOfPersons`). Not a live capture yet.

## Campaign pages (live captures, 2026-11-02, logged in, read-only GETs)

The campaign "tabs" are sections on `/tilbud`. Find them with
`GET https://www.nemlig.com/tilbud?GetAsJson=1&d=1`: in `content[]`, each item with
`TemplateName` `productlistshowallspot` has a `Heading` and a `ProductGroupId`
(and `TotalProducts`). The front page (`/?GetAsJson=1&d=1`) only has a "Skarp pris"
spot, with a different group id than `/tilbud`. Match on `Heading`, not on ids:
ids are Sitecore items and can change.

| File | `Heading` on /tilbud | `ProductGroupId` (2026-11-02) | `NumFound` |
|---|---|---|---|
| `offers_skarp_pris.json` | Skarp pris | `106c6141-1fca-4a06-8b02-8c9df6219c5a` | 61 |
| `offers_priskup.json` | Ugens Priskup | `19046154-e839-4d3c-ad7f-9b5e4c9f9a9a` | 200 |
| `offers_dagstilbud.json` | Dagstilbud | `19b12525-8929-4857-a271-ac61cea61309` | 15 |
| `offers_madspild.json` | Undgå madspild | `b2dee8ec-1194-4ab2-8e2a-649be1477e32` | 8 |
| `offers_gratis.json` | En gave til dig | `3db1e100-3e50-4848-9562-dd0a9734e89c` | 0 |

There is no "Gratis" section or page on the website (`/tilbud/gratis`, `/gratis` are 404;
`/tilbud/gave` is a "Gratis Daim" landing page that only shows Skarp pris). The closest is
"En gave til dig", which was empty for this account. Free gifts do show up in normal search
(`search_gift.json`).

Products come from (same call the site makes, `getPaginatedProducts$` in `main.js`):

    GET https://www.nemlig.com/webapi/{CombinedProductsAndSitecoreTimestamp}/{TimeslotUtc}/{DeliveryZoneId}/{UserId}/Products/GetByProductGroupId
        ?productGroupId=<id>&pageIndex=0&pagesize=10   (optional: sortorder)

`CombinedProductsAndSitecoreTimestamp` is from `/webapi/v2/AppSettings/Website`; `TimeslotUtc`,
`DeliveryZoneId`, `UserId` are in `Settings` of any `?GetAsJson=1` page. `UserId` is personal:
never log or store it.

Answer: `{Products, ProductGroupId, Start, NumFound, ExceptionMessage, Advertisements}`.
`Advertisements` are banners `{Key: position, Value: {...}}` that take list slots, so a page of
10 can hold fewer than 10 products. Fixtures keep at most 10 products.

Product fields (same shape as search hits):

- `Id`, `Name`, `Brand`, `Description` (size; for madspild also "Sidste anv. dato: dd-mm-yyyy")
- `Price`: normal price. `UnitPriceCalc` + `UnitPriceLabel`: normal unit price.
- `CampaignAttribute`: "Priskup", "Dagstilbud", "Undgå madspild" (missing on Skarp pris).
- `Campaign.CampaignPrice` / `CampaignUnitPrice`: offer price. `Price` is the before price.
- `Campaign.DiscountSavings`, `Campaign.DiscountPercent` (only `ProductCampaignDiscountPercent`).
- `Campaign.Type`: `ProductCampaignDiscount`, `ProductCampaignDiscountPercent`,
  `ProductCampaignMixOffer`, `ProductCampaignFreeProduct`. `Campaign.Code`: e.g. US, P, D, UM, U.
- `Campaign.MaxQuantity`: max per order (0 = no limit).
- `Campaign.IntervalStart` / `IntervalEnd`: UTC; end date = `IntervalEnd`.
- No offer text field: build it from type + prices.

Odd case: a dagstilbud item had `Price` 82.565 < `CampaignPrice` 84.95 and negative
`DiscountSavings` (see `offers_dagstilbud.json`). Do not trust that an offer is cheaper.

## `search_gift.json`

`GET https://webapi.prod.knl.nemlig.it/searchgateway/api/search?query=mums rosinboller&take=10&skip=0&recipeCount=0&timestamp=…&timeslotUtc=…&deliveryZoneId=…`,
trimmed to product 5606321 (Facets, Ads, Recipes emptied). Free gift: `Price` 20.45 but
`Campaign` = `{Type: ProductCampaignFreeProduct, Code: U, CampaignPrice: 0, MaxQuantity: 1,
IntervalEnd: 2026-12-31T22:59:59Z}`, `SubCategory` "Gave fra nemlig.com".
