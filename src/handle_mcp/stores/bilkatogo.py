"""BilkaToGo: the private API bilkatogo.dk uses. See docs/research.md.

Login: Gigya accounts.login → accounts.getJWT (24 h) → POST api.bilkatogo.dk/api/auth/LoginJWT.
Search goes to the website's public Algolia index. Money comes in øre.
Facts and fixtures from nickknissen/bilkatogo (MIT, Copyright (c) 2026 Nick Knissen).
Never add order, payment or delivery-address calls here (docs/adr/0001).
"""

from __future__ import annotations

import asyncio
import json
import logging
import re
from datetime import date, datetime, time, timedelta
from decimal import Decimal, InvalidOperation
from typing import Any
from urllib.parse import urlencode
from zoneinfo import ZoneInfo

import httpx

from handle_mcp.models import (
    Cart,
    CartLine,
    DeliveryInfo,
    Order,
    Product,
    QuantityResult,
    Recipe,
    StoreTerms,
)
from handle_mcp.stores import StoreError, load_login

RETRYABLE = (httpx.TimeoutException, httpx.NetworkError, httpx.RemoteProtocolError)
log = logging.getLogger(__name__)

GIGYA = "https://accounts.eu1.gigya.com"
GIGYA_API_KEY = "3_tA6BbV434FQqN73HnUG1KA3qFv8KiG4OqLu9eWPh7sKRqRizH5Vfv5Larmgrb4I2"
GIGYA_BAD_LOGIN = 403042
API = "https://api.bilkatogo.dk/api"
ALGOLIA = "https://f9vbjlr1bk-dsn.algolia.net/1/indexes/*/queries"
ALGOLIA_HEADERS = {
    "x-algolia-application-id": "F9VBJLR1BK",
    "x-algolia-api-key": "1deaf41c87e729779f7695c00f190cc9",  # public search-only key
}
ALGOLIA_INDEX = "prod_BILKATOGO_PRODUCTS"
JWT_SECONDS = 86400
FEE_GROUPS = {"Emballage", "Services"}  # cart groups that are fees, not products
TZ = ZoneInfo("Europe/Copenhagen")
RETRIES = 2
UNITS = {"kg": "kg", "ltr": "l", "l": "l", "stk": "stk"}
STATUS = {  # Bilka's codes → the words the website shows
    "Bestilt": "Modtaget",
    "Ubehandlet": "Under behandling",
    "Pakket": "Pakket",
    "Leveret": "Leveret",
    "Faktureret": "Leveret",
    "Annulleret": "Annulleret",
}
NOT_HOME = "Kurven er sat til afhentning, ikke hjemlevering. Vælg hjemlevering på bilkatogo.dk."


GENERIC_OFFERS = {"Se Her"}  # offerDescription with no real text (docs/research.md)


def _kr(ore: Any) -> Decimal:
    try:
        return Decimal(str(ore or 0)).scaleb(-2)
    except (InvalidOperation, ValueError) as exc:
        raise StoreError(f"BilkaToGo sendte et ukendt beløb: {ore!r}.") from exc


def _is_home(address: dict) -> bool:
    """Home addresses have a DAWA id or a recipient; pickup points (Bilka stores) do not.
    ⚠ Confirm with a live home-delivery cart (docs/research.md)."""
    return bool(address.get("dawa_uid") or address.get("recipient_name"))


def _slot(day: str | None, start: str | None, end: str | None) -> tuple[datetime | None, ...]:
    try:
        d = date.fromisoformat(day or "")
        return tuple(datetime.combine(d, time.fromisoformat(t), TZ) for t in (start, end))
    except (TypeError, ValueError):
        return None, None


def _lines(groups: list[dict]) -> list[CartLine]:
    """Cart and order lines are nested: groups → blocks → orderlines."""
    return [
        CartLine(
            str((ln.get("product") or {}).get("objectID", "")),
            (ln.get("product") or {}).get("name", ""),
            int(ln.get("quantity") or 0),
            _kr(ln.get("amount")),
        )
        for group in groups or []
        if group.get("headline") not in FEE_GROUPS
        for block in group.get("lines") or []
        for ln in block.get("orderlines") or []
    ]


class _Refused(StoreError):
    """Bilka answered HTTP 200 with an error body."""


class BilkaStore:
    name = "bilkatogo"

    def __init__(self, client: httpx.AsyncClient | None = None) -> None:
        self._client = client or httpx.AsyncClient(timeout=30)
        self._jwt: str | None = None
        self._jwt_until = datetime.min.replace(tzinfo=TZ)
        self._lock = asyncio.Lock()

    async def _send(self, method: str, url: str, **kwargs: Any) -> httpx.Response:
        for attempt in range(RETRIES + 1):
            try:
                resp = await self._client.request(method, url, **kwargs)
            except RETRYABLE as exc:  # safe: every write sets an absolute quantity
                if attempt == RETRIES:
                    raise StoreError(f"BilkaToGo svarer ikke ({type(exc).__name__}).") from exc
            except httpx.HTTPError as exc:
                raise StoreError(f"Kan ikke nå BilkaToGo ({type(exc).__name__}).") from exc
            else:
                if resp.status_code != 429 and resp.status_code < 500:
                    return resp
                if attempt == RETRIES:
                    raise StoreError(f"BilkaToGo svarer {resp.status_code}. Prøv igen senere.")
            await asyncio.sleep(0.5 * 2**attempt)
        raise AssertionError("unreachable")

    @staticmethod
    def _json(resp: httpx.Response, what: str) -> dict:
        try:
            data = resp.json()
        except ValueError:
            data = None
        if not isinstance(data, dict):
            raise StoreError(f"BilkaToGo ({what}) svarede uventet. API'et kan have ændret sig.")
        return data

    async def _gigya(self, method: str, form: dict[str, str]) -> dict:
        data = self._json(await self._send("POST", f"{GIGYA}/{method}", data=form), method)
        if data.get("errorCode") == GIGYA_BAD_LOGIN:
            raise StoreError("BilkaToGo afviser login. Tjek email og kodeord.")
        if data.get("errorCode", 0) != 0:
            raise StoreError(
                f"BilkaToGo login fejlede: {data.get('errorMessage', data['errorCode'])}"
            )
        return data

    async def _login(self) -> None:
        login = load_login(self.name)
        form = {"loginID": login["email"], "password": login["password"], "apiKey": GIGYA_API_KEY}
        session = (await self._gigya("accounts.login", form | {"httpStatusCodes": "true"})).get(
            "sessionInfo"
        )
        token = (session or {}).get("cookieValue")
        jwt_form = {
            "apiKey": GIGYA_API_KEY,
            "login_token": token or "",
            "expiration": str(JWT_SECONDS),
        }
        jwt = (await self._gigya("accounts.getJWT", jwt_form)).get("id_token")
        if not token or not jwt:
            raise StoreError("BilkaToGo login svarede uden token. API'et kan have ændret sig.")
        # Never send the `cart` cookie: LoginJWT would REPLACE the account's cart with it.
        self._client.cookies.delete("cart")
        resp = await self._send(
            "POST", f"{API}/auth/LoginJWT", params={"u": "w"}, headers={"jwt_token": jwt}
        )
        if resp.status_code != 200:
            raise StoreError(f"BilkaToGo login fejlede ({resp.status_code}).")
        self._jwt = jwt
        self._jwt_until = datetime.now(TZ) + timedelta(seconds=JWT_SECONDS - 300)
        log.info("logged in to BilkaToGo")

    async def _shop(self, method: str, path: str, params: dict[str, Any] | None = None) -> dict:
        for fresh in (False, True):
            used = self._jwt
            async with self._lock:
                # Skip a fresh login if another call already logged in while we waited.
                stale = fresh and self._jwt == used
                if stale or not self._jwt or datetime.now(TZ) >= self._jwt_until:
                    await self._login()
            resp = await self._send(
                method,
                f"{API}/{path}",
                params={"u": "w", **(params or {})},
                headers={"jwt_token": self._jwt or ""},
            )
            if resp.status_code == 401:
                continue
            if resp.status_code >= 400:
                raise StoreError(f"BilkaToGo afviste {path} ({resp.status_code}).")
            data = self._json(resp, path)
            if data.get("code") == 1:  # {"code": 1, "msg": "Du er ikke logget ind"}
                continue
            if "msg" in data and "code" in data:
                raise _Refused(data["msg"])
            return data
        raise StoreError("BilkaToGo afviser en frisk session. Tjek login.")

    async def _cart_json(self) -> dict:
        return await self._shop("GET", "shop/v6/Cart", {"extra": "deliveryAddress,deliveryDate"})

    async def search(self, query: str, limit: int = 10) -> list[Product]:
        address = (await self._cart_json()).get("deliveryAddress") or {}
        sid = str(address.get("sid") or "")
        if not sid:
            raise StoreError(
                "Ingen leveringsadresse valgt i BilkaToGo. Vælg hjemlevering på bilkatogo.dk."
            )
        params = urlencode(
            {
                "query": query,
                "hitsPerPage": limit,
                # Algolia needs every word by default: "cheasy skyr passionsfrugt" gives 0 hits
                # when that flavour is not sold. Then retry with fewer words, like nemlig does.
                "removeWordsIfNoResults": "allOptional",
                "filters": f"isInAssortmentIn:{sid} AND nonsearchable:false",
                "attributesToRetrieve": json.dumps(
                    [
                        "name",
                        "brand",
                        "gtin",
                        "netcontent",
                        "productType",
                        "deepestCategoryPath",
                        "price",  # the price without the offer
                        f"storeData.{sid}",
                    ]
                ),
            }
        )
        body = {"requests": [{"indexName": ALGOLIA_INDEX, "params": params}]}
        resp = await self._send("POST", ALGOLIA, headers=ALGOLIA_HEADERS, json=body)
        if resp.status_code != 200:
            raise StoreError(f"BilkaToGo-søgning fejlede ({resp.status_code}).")
        try:
            hits = self._json(resp, "søgning")["results"][0]["hits"]
        except (KeyError, IndexError, TypeError) as exc:
            raise StoreError("BilkaToGo-søgning har ændret format.") from exc
        return [p for p in (self._product(h, sid) for h in hits) if p]

    async def recipes(self, query: str, limit: int = 5) -> list[Recipe]:
        raise NotImplementedError  # no recipe API found in BilkaToGo yet

    def _product(self, hit: dict, sid: str) -> Product | None:
        store = (hit.get("storeData") or {}).get(sid)
        if not store:
            return None
        unit = (store.get("unitsOfMeasurePriceUnit") or "").rstrip(".").lower()
        now, normal = _kr(store.get("price")), _kr(hit.get("price"))
        before = normal if normal > now else None
        offer = store.get("offerDescription") or None
        if offer in GENERIC_OFFERS:  # a leaflet offer with no text of its own
            offer = f"Tilbud, før {before:.2f} kr".replace(".", ",") if before else "Tilbud"
        shown = store.get(
            "unitsOfMeasureShowPrice"
        )  # with the offer; unitsOfMeasurePrice is without
        return Product(
            store=self.name,
            id=str(hit["objectID"]),
            name=hit.get("name", ""),
            price=now,  # what you pay now, offer included
            brand=hit.get("brand") or None,
            size=hit.get("netcontent") or None,
            unit_price=_kr(shown or store.get("unitsOfMeasurePrice")) if unit else None,
            unit=UNITS.get(unit, unit or None),
            ean=hit.get("gtin") or None,
            on_offer=bool(offer),
            offer_text=offer,
            before_price=before if offer else None,
            available=bool(store.get("inStock")),
            category=" > ".join(
                x for x in (hit.get("deepestCategoryPath"), hit.get("productType")) if x
            )
            or None,
        )

    def _cart(self, data: dict) -> Cart:
        stat, address = data.get("stat") or {}, data.get("deliveryAddress") or {}
        lines = _lines(data.get("lines"))
        fees = {
            name: _kr(stat.get(key))
            for name, key in (
                ("levering", "delivery_price"),
                ("pakkeservice", "packing"),
                ("emballage", "packaging_price"),
                ("pant", "deposit"),
            )
            if stat.get(key)
        }
        warnings = [] if _is_home(address) else [NOT_HOME]
        if stat.get("minimum_left_text"):
            warnings.append(re.sub(r"<[^>]+>", "", stat["minimum_left_text"]))
        warnings += [
            m.get("text", str(m)) if isinstance(m, dict) else str(m)
            for m in data.get("messages") or []
        ]
        return Cart(
            store=self.name,
            lines=lines,
            products_total=sum((ln.line_total for ln in lines), Decimal(0)),
            fees=fees,
            total=_kr(stat.get("price")),
            minimum_order=_kr(address["minimum_buy"]) if address.get("minimum_buy") else None,
            minimum_met=not stat.get("minimum_left"),
            delivery=(data.get("deliveryDate") or {}).get("delivery_message") or None,
            warnings=warnings,
        )

    async def get_cart(self) -> Cart:
        return self._cart(await self._cart_json())

    async def set_quantities(self, items: dict[str, int]) -> list[QuantityResult]:
        results = []
        for pid, qty in items.items():
            try:
                answer = await self._shop(
                    "GET",
                    "shop/v6/ChangeLineCount",
                    {"productId": pid, "count": qty, "fullCart": 0},
                )
            except _Refused as exc:  # Bilka's own message; other errors stop the loop
                results.append(QuantityResult(pid, qty, None, False, str(exc)))
                continue
            applied = answer.get("productAmount")
            note = (
                " ".join(m for m in (answer.get("message"), answer.get("offerLimitMessage")) if m)
                or None
            )
            ok = applied == qty
            if not ok:
                note = note or f"BilkaToGo satte antallet til {applied}, ikke {qty}."
            results.append(
                QuantityResult(pid, qty, applied if isinstance(applied, int) else None, ok, note)
            )
        return results

    async def terms(self) -> StoreTerms:
        data = await self._cart_json()
        cart, address = self._cart(data), data.get("deliveryAddress") or {}
        return StoreTerms(
            store=self.name,
            home_delivery=_is_home(address),
            delivery_fee=cart.fees.get("levering"),
            other_fees={k: v for k, v in cart.fees.items() if k != "levering"},
            minimum_order=cart.minimum_order,
        )

    async def delivery_info(self) -> DeliveryInfo:
        data = await self._cart_json()
        address, chosen = data.get("deliveryAddress") or {}, data.get("deliveryDate") or {}
        start, end = _slot(
            chosen.get("deliveryDate"), chosen.get("intervalStart"), chosen.get("intervalEnd")
        )
        if end and end < datetime.now(TZ):
            start = end = None  # an old slot from a past order is not a choice
        nxt = address.get("nextAvailableDeliveryDate")
        return DeliveryInfo(
            store=self.name,
            chosen_start=start,
            chosen_end=end,
            next_available=date.fromisoformat(nxt) if nxt else None,
            note=None if _is_home(address) else NOT_HOME,
        )

    def _order(self, o: dict, lines: list[CartLine] | None = None) -> Order:
        start, end = _slot(
            o.get("deliveryDate"),
            *((o.get("deliveryInterval") or {}).get(k) for k in ("start", "end")),
        )
        ordered = o.get("ordered") or {}
        placed = (
            f"{ordered.get('date')}T{ordered.get('time') or '00:00:00'}"
            if ordered.get("date")
            else None
        )
        return Order(
            store=self.name,
            id=str(o.get("orderId", "")),
            status=STATUS.get(o.get("status", ""), o.get("status") or "ukendt"),
            total=_kr(o.get("packedPrice") or o.get("orderedPrice") or o.get("price")),
            placed_at=datetime.fromisoformat(placed).replace(tzinfo=TZ) if placed else None,
            delivery_start=start,
            delivery_end=end,
            lines=lines or [],
        )

    async def orders(self, limit: int = 10) -> list[Order]:
        params = {"limit": limit, "offset": 0, "muah": 1, "ordertype": "dibs,pgw"}
        data = await self._shop("POST", "shop/v7/OrderHistory", params)
        return [self._order(o) for o in data.get("orders") or []]

    async def order(self, order_id: str) -> Order:
        details = (await self._shop("GET", "shop/v7/OrderDetails", {"orderId": order_id})).get(
            "order"
        )
        if not details:
            raise StoreError(f"Fandt ikke ordre {order_id} hos BilkaToGo.")
        return self._order(details, _lines(details.get("lines")))
