# Research: nemlig.com og BilkaToGo

Trin 1 af v1. Skrevet 2026-11-01.

**Kilder:** kode, docs, tests og fixtures i
[eisbaw/nemlig_cli](https://github.com/eisbaw/nemlig_cli) (`nemlig_api.md`),
[kraenhansen/nemlig-mcp](https://github.com/kraenhansen/nemlig-mcp),
[nickknissen/bilkatogo](https://github.com/nickknissen/bilkatogo) (fixtures er rigtige, redigerede svar) og
[stigvoss/bilkatogo-mcp](https://github.com/stigvoss/bilkatogo-mcp) (ingen licens: kun læst for fakta).

Skrevet før de første live kald. Punkter markeret **⚠ live** er stadig ikke bekræftet med en rigtig konto; de står samlet under "Åbne spørgsmål" sidst. Det, der er bekræftet siden, står under "Bekræftet live".

## Kort svar

| Spørgsmål | nemlig.com | BilkaToGo |
|---|---|---|
| Login | 3 kald: XSRF → Bearer → `/webapi/login` | 3 kald: Gigya login → Gigya JWT → `LoginJWT` |
| Session holder | `.ASPXAUTH`-cookie: 1 år. Bearer-token: 5 min | Bilka-JWT: 24 t. Log ind igen med email/kode |
| Læse ledige tider | **Ikke fundet** | **Delvist:** `nextAvailableDeliveryDate` på kurvens adresse. Ingen liste |
| Valgt tid | `GET /webapi/Order/DeliverySpot` → `TimeSlot` | Kurvens `deliveryDate` → `intervalStart/End` |
| Gebyrer fra API | Ja: `DeliveryPrice` i kurv | Ja: `stat.delivery_price`, `stat.packing` (Pakkeservice) |
| Minimumskøb fra API | Ja: `MinimumOrderTotal`, `IsMinTotalValid` | Ja: `stat.minimum_left`, `deliveryAddress.minimum_buy` |
| Hjemlevering eller afhentning | Altid hjemlevering | Kurven har `hd_available`, `missing_hd_address` |
| EAN/GTIN i søgning | **Nej** (ikke i kendte svar) | **Ja:** `gtin` i Algolia-hit |

## nemlig.com

### Login og session
1. `GET /webapi/AntiForgery` → `X-XSRF-TOKEN` + cookie.
2. `GET /webapi/Token` → Bearer JWT, `expires_in: 300` (5 min).
3. `POST /webapi/login` med XSRF + Bearer → cookie `.ASPXAUTH` (1 år).

Konsekvens: gem kun cookies lokalt (`~/.config/handle-mcp/nemlig.json`, chmod 600). Hent ny Bearer ved 401. Log ind igen, kun hvis cookien er død.

Krævede headers: `Platform: web`, `Device-Size: desktop`, `Version: <app-version>`, `X-Correlation-Id: <uuid4>`. Versionen kan ændre sig. **⚠ live:** hvad sker der med en gammel `Version`?

### Søgning
`GET https://webapi.prod.knl.nemlig.it/searchgateway/api/search?query=…&take=…&timeslotUtc=…&deliveryZoneId=…`

- `timeslotUtc` og `deliveryZoneId` kommer fra page settings. Pris og lager kan altså afhænge af tid og zone.
- Svaret har `Id` (nemligs varenummer), navn, brand, pris, `UnitPriceLabel` (fx "kr./Kg."), kampagne (`ProductCampaignBuyXForY`, `ProductCampaignMixOffer`).
- **Ingen EAN/GTIN** i de kendte svar. **⚠ live:** tjek produktsiden (`/webapi/…/product`) for EAN. Ellers: match på navn + mængde + brand i v1. EAN-match bliver kun muligt for Bilka.

**Tilbud (set live 2026-11-01):**
- `DiscountItem: true` uden `Campaign` er nemligs faste "Discount"-mærke (står også i `Labels`). Det er ikke et tilbud.
- `ProductCampaignDiscountPercent` og `ProductCampaignDiscount`: `Price` er prisen FØR tilbud. Du betaler `CampaignPrice`, og enhedsprisen er `CampaignUnitPrice`.
- `ProductCampaignMixOffer` og `ProductCampaignBuyXForY`: `Price` er stykprisen. Tilbuddet er `MinQuantity` for `TotalPrice`.
- Kampagner vises allerede aftenen før `IntervalStart`.

### Kurv
- `GET /webapi/basket/GetBasket`. Svaret er stort (~1,5 KB pr. linje + analytics). Returnér kun udvalgte felter som standard (kraenhansen: `BASKET_FIELDS`).
- Nyttige felter: `TotalPrice`, `DeliveryPrice`, `TotalBagsPrice`, `TotalDepositsPrice`, `MinimumOrderTotal`, `IsMinTotalValid`, `FormattedDeliveryTime`, `ValidationFailures`.
- `POST /webapi/basket/AddToBasket` med `ProductId`, `quantity`. Antallet er **absolut**. 0 fjerner linjen.
- Persondata i kurven: `InvoiceAddress`, `DeliveryAddress`, `CustomerName`, `MobileNumber`, `Email`, `DoorCode`, `UnattendedNotes`. Skal fjernes.

### Leveringstider
- Kun `GET /webapi/Order/DeliverySpot` er kendt. Den viser den **valgte** tid (`TimeSlot`, `EditDeadline`) for den åbne ordre. Den har også `CustomerName` → privacy-filter.
- Intet kendt endpoint lister ledige tider. **⚠ live:** åbn tidsvælgeren på nemlig.com med DevTools og find læse-kaldet.
- v1-beslutning: `get_delivery_info` viser valgt tid + `EditDeadline`. Ingen valg af tid.

### Ordrer
Kendt i `nemlig_cli` (ordrehistorik og ordredetaljer). Indeholder adresse og navn → privacy-filter.

### Endpoints, der aldrig må være i koden
`/webapi/Order/PlaceOrderLoggedIn`, `/webapi/Checkout/GetCreditCards`, `/webapi/Checkout/GetCardsFees`. En test skal grep'e efter dem.

## BilkaToGo

### Login og session
1. `POST https://accounts.eu1.gigya.com/accounts.login` (offentlig `GIGYA_API_KEY`) med email/kode → `login_token`.
2. `POST accounts.eu1.gigya.com/accounts.getJWT` → Gigya JWT.
3. `POST https://api.bilkatogo.dk/api/auth/LoginJWT` → Bilka-session (cookies + JWT, 24 t).

- Forny 5 min før udløb (nickknissen: `JWT_RENEWAL_MARGIN`).
- Udløbet session: fejl-body `{"code": 1, "msg": "Du er ikke logget ind"}`. **Kun** den betyder "log ind igen". Andre fejl-bodies er rigtige fejl.
- **Fælde:** `LoginJWT` erstatter kontoens kurv med kurven bag `cart`-cookien. Send aldrig en `cart`-cookie ved login.

### Søgning
Algolia, ikke Bilkas eget API. App `F9VBJLR1BK`, index `prod_BILKATOGO_PRODUCTS`, offentlig søgenøgle. Hardkod dem.

Nyttige felter pr. hit: `objectID` (varenummer til kurven), `gtin` (**EAN findes**), `name`, `brand`, `netcontent`, `units`, `uom`, `unitOfMeasurePrice` (pris pr. kg/l), `storeData[<sid>]` (pris, tilbud, lager pr. butik), `isInOffer`, `cpOfferPrice`, `is_multibuy`.

- Pris og lager afhænger af butik-id (`sid`). For hjemlevering: brug `sid` fra kurvens `deliveryAddress`.
- Brug `objectID`, ikke `article`, som varenummer (nickknissen v0.3.0 breaking change).

**Tilbud (set live 2026-11-01, beløb i øre):**
- `storeData.<butik>.price` er det, du betaler. Top-feltet `price` er prisen uden tilbud. `beforePrice` er altid 0.
- `unitsOfMeasurePrice` er enhedsprisen uden tilbud. `unitsOfMeasureShowPrice` er den med tilbud.
- `offerDescription` "Se Her" er et avistilbud uden egen tekst. "Skarp pris" er også et tilbud med lavere pris.
- `offerFrom` og `offerUntil` er altid tomme. Derfor kender vi ikke slutdatoen for et Bilka-tilbud, og et tilbud tæller med, selvom det slutter før levering.
- Mængdetilbud på tværs af varer ("Mix 10 for 25" på bananer og pærer) regnes kun inden for én linje.

### Kurv
- Læs: `GET shop/v6/Cart?u=w&extra=deliveryAddress,deliveryDate`. Tilføj `without_cache=1`, når du tjekker en ændring.
- Skriv: `GET shop/v6/ChangeLineCount?u=w&productId=<objectID>&count=<n>&fullCart=0`. Antal er **absolut**. 0 fjerner.
- Svaret er HTTP 200, også ved fejl. Afvist = fejl-body (fx `eid: 410`) **eller** `productAmount` ≠ ønsket antal. `message`/`offerLimitMessage` på en godkendt ændring er en advarsel.
- Beløb er i **øre**.
- Gebyrer i `stat`: `delivery_price`, `packing` (Pakkeservice, 3900 øre = 39 kr i fixture), `packaging_price`, `deposit`. Linjerne "Papirpose" og "Pakkeservice" er gebyrer, ikke varer.
- Minimumskøb: `stat.minimum_left` (+ tekst) og `deliveryAddress.minimum_buy`.
- Hjemlevering: kurven har `hd_available` og `missing_hd_address`. Fixturen er afhentning (`hd_available: false`). **Bekræftet live 2026-11-01:** `delivery_price` = 39 kr + pakkeservice 39 kr, minimum 500 kr, `deliveryAddress` = hjemmeadresse (filtreres).
- Skift til hjemlevering ændrer tilstand (`SetDeliveryAddress`). v1 gør det **ikke**. Er kurven sat til afhentning, svarer tool'et klart: "Vælg hjemlevering på bilkatogo.dk".

### Leveringstider
- Valgt tid: kurvens `deliveryDate` (`deliveryDate`, `intervalStart`, `intervalEnd`, `delivery_message`).
- Næste ledige dag: `deliveryAddress.nextAvailableDeliveryDate`. Det er et læse-felt.
- Liste over ledige tider: **ikke fundet**. nickknissen har det udtrykkeligt uden for scope. **⚠ live:** find kaldet bag tidsvælgeren i DevTools.

### Ordrer
`POST shop/v7/OrderHistory`, `GET shop/v7/OrderDetails`. Status-koder (`Bestilt`, `Leveret`, `Faktureret`) mappes til hjemmesidens ord (nickknissen ADR 0003). Ordrer kan have `home_delivery` og adresse → privacy-filter.

## Konsekvenser for v1

1. **Varematch:** EAN kun hos Bilka. `compare_basket` matcher på normaliseret navn + mængde + enhed, og bruger EAN, når begge sider har det. Usikre match markeres.
2. **Gebyrer og minimumskøb** kommer fra API'et hos begge butikker. Ingen config nødvendig, kun som fallback.
3. **Leveringstider:** v1 viser valgt tid (begge) og næste ledige dag (Bilka). Ingen liste og intet valg.
4. **Privacy-filter** skal dække: `InvoiceAddress`, `DeliveryAddress`/`deliveryAddress` (kun ved hjemlevering), navne-, email-, telefon-felter, `DoorCode`, `UnattendedNotes`, `uid`, `tid`, `CustomerName`.
5. **Forbudte endpoints-test:** grep efter `PlaceOrder`, `Checkout/`, `SetDeliveryAddress`, `EmptyCart` i `src/`.

## Bekræftet live (2026-11-01, hjemlevering valgt)

- **BilkaToGo `Cart`:** `stat.delivery_price` = 39 kr (levering) + 39 kr (pakkeservice), minimumskøb 500 kr. Hjemlevering ses på `deliveryAddress.dawa_uid` / `recipient_name`.
- **nemlig:** leveringsgebyret afhænger af den valgte tid (19-51 kr). Aflæst i tidsvælgeren på hjemmesiden, ikke fra et API-felt. Tiderne kan ses uden login.
- **Privacy:** hele `deliveryAddress`-blokken fjernes af `privacy.scrub()`, så `dawa_uid` og `recipient_name` når aldrig modellen.

## Åbne spørgsmål

Hvis du finder svaret i DevTools → Network (Fetch/XHR): kopier kun URL og svar-felter, aldrig cookies, og læg et redigeret svar i `tests/fixtures/`.

1. **nemlig:** hvilket kald henter de ledige leveringstider? Er det et GET (kun læse)?
2. **nemlig:** står EAN/GTIN på produktsiden (`/webapi/…/product`)? Søgesvaret har det ikke.
3. **BilkaToGo:** hvilket kald henter de ledige tider for hjemlevering?
4. **nemlig:** hvad svarer API'et på en forældet `Version`-header? (I dag: `HANDLE_NEMLIG_APP_VERSION` kan sættes som env var.)

Indtil da viser v1 kun den valgte tid (begge) og næste ledige dag (Bilka), og matcher på navn + mængde i stedet for EAN hos nemlig.
