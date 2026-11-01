"""nemlig.com: the private web API the website uses. See docs/research.md.

Login: XSRF token → short Bearer token (5 min) → POST /webapi/login, which sets the
.ASPXAUTH cookie (1 year). The Bearer token is renewed on 401.
Never add order, checkout or payment endpoints here (docs/adr/0001).
"""

from __future__ import annotations

import asyncio
import logging
import os
import time
import uuid
from datetime import datetime
from decimal import Decimal
from typing import Any

import httpx

from handle_mcp.models import (
    Cart,
    CartLine,
    DeliveryInfo,
    Order,
    Product,
    QuantityResult,
    StoreTerms,
)
from handle_mcp.stores import StoreError, load_login

RETRYABLE = (httpx.TimeoutException, httpx.NetworkError, httpx.RemoteProtocolError)
log = logging.getLogger(__name__)
logging.getLogger("httpx").setLevel(logging.WARNING)  # its INFO logs full URLs with tokens

BASE = "https://www.nemlig.com"
SEARCH = "https://webapi.prod.knl.nemlig.it/searchgateway/api/search"
APP_VERSION = os.environ.get("HANDLE_NEMLIG_APP_VERSION", "11.201.0")
SETTINGS_TTL = 600  # seconds to reuse timeslot/zone for search
RETRIES = 2  # extra attempts on timeout, 429 and 5xx
STATUS = {1: "Bestilt", 2: "Under behandling", 4: "Leveret"}
UNITS = {"kg": "kg", "l": "l", "ltr": "l", "stk": "stk"}


def _money(value: Any) -> Decimal | None:
    return None if value is None else Decimal(str(value))


def _time(value: str | None) -> datetime | None:
    if not value or value.startswith(("0001-", "9999-")):
        return None
    return datetime.fromisoformat(value)


def _unit(label: str | None) -> str | None:
    """'kr./Kg.' → 'kg', 'kr/l' → 'l', 'kr/stk' → 'stk'."""
    if not label:
        return None
    tail = label.lower().replace(".", "").split("/")[-1].strip()
    return UNITS.get(tail, tail or None)


def _offer(campaign: dict | None) -> str | None:
    if not campaign:
        return None
    qty, price = campaign.get("MinQuantity"), campaign.get("TotalPrice")
    if qty and qty > 1 and price:
        return f"{qty} for {price} kr"
    return campaign.get("Type")


class NemligStore:
    name = "nemlig"

    def __init__(self, client: httpx.AsyncClient | None = None) -> None:
        self._client = client or httpx.AsyncClient(timeout=20, follow_redirects=True)
        self._bearer: str | None = None
        self._xsrf: str | None = None
        self._logged_in = False
        self._settings: tuple[float, dict[str, Any]] | None = None
        self._lock = asyncio.Lock()

    # --- HTTP -------------------------------------------------------------

    def _headers(self) -> dict[str, str]:
        headers = {
            "Accept": "application/json, text/plain, */*",
            "Device-Size": "desktop",
            "Platform": "web",
            "Version": APP_VERSION,
            "X-Correlation-Id": str(uuid.uuid4()),
            "Referer": f"{BASE}/",
            "User-Agent": "Mozilla/5.0 (handle-mcp)",
        }
        if self._bearer:
            headers["Authorization"] = f"Bearer {self._bearer}"
        if self._xsrf:
            headers["X-XSRF-TOKEN"] = self._xsrf
        return headers

    async def _send(self, method: str, url: str, **kwargs: Any) -> Any:
        """One request with retries on timeout/429/5xx. Returns parsed JSON."""
        for attempt in range(RETRIES + 1):
            try:
                resp = await self._client.request(method, url, headers=self._headers(), **kwargs)
            except RETRYABLE as exc:  # safe: every write sets an absolute quantity
                if attempt == RETRIES:
                    raise StoreError(f"nemlig.com svarer ikke ({type(exc).__name__}).") from exc
            except httpx.HTTPError as exc:
                raise StoreError(f"Kan ikke nå nemlig.com ({type(exc).__name__}).") from exc
            else:
                if resp.url.host.endswith("queue-it.net"):
                    raise StoreError("nemlig.com har kø lige nu (queue-it). Prøv igen om lidt.")
                if resp.status_code == 429 or resp.status_code >= 500:
                    if attempt == RETRIES:
                        raise StoreError(f"nemlig.com svarer {resp.status_code}. Prøv igen senere.")
                elif resp.status_code == 401:
                    raise _Unauthorized
                elif resp.status_code >= 400:
                    raise StoreError(f"nemlig.com afviste kaldet ({resp.status_code}): {url}")
                else:
                    try:
                        return resp.json() if resp.content else None
                    except ValueError as exc:
                        raise StoreError(
                            "nemlig.com svarede ikke med JSON. API'et kan have ændret sig."
                        ) from exc
            await asyncio.sleep(0.5 * 2**attempt)
        raise AssertionError("unreachable")

    async def _api(self, method: str, url: str, **kwargs: Any) -> Any:
        """A logged-in call. On 401: renew the Bearer token, then log in again."""
        async with self._lock:
            if not self._logged_in:
                await self._login()
        for renew in (self._renew_token, self._login, None):
            try:
                return await self._send(method, url, **kwargs)
            except _Unauthorized:
                if renew is None:
                    raise StoreError("nemlig.com afviser login. Tjek email og kodeord.") from None
                try:
                    await renew()
                except _Unauthorized:
                    continue
        raise AssertionError("unreachable")

    async def _renew_token(self) -> None:
        self._bearer = None
        token = await self._send("GET", f"{BASE}/webapi/Token")
        self._bearer = token["access_token"]
        xsrf = await self._send("GET", f"{BASE}/webapi/AntiForgery")
        self._xsrf = xsrf["Value"]

    async def _login(self) -> None:
        login = load_login(self.name)
        self._bearer = self._xsrf = None
        try:
            await self._renew_token()
            result = await self._send(
                "POST",
                f"{BASE}/webapi/login",
                json={
                    "Username": login["email"],
                    "Password": login["password"],
                    "CheckForExistingProducts": True,
                    "DoMerge": True,
                    "AppInstalled": False,
                    "SaveExistingBasket": False,
                },
            )
        except _Unauthorized:
            raise StoreError("nemlig.com afviser login. Tjek email og kodeord.") from None
        if not isinstance(result, dict) or "RedirectUrl" not in result:
            raise StoreError("nemlig.com login fejlede. Tjek email og kodeord.")
        await self._renew_token()  # tokens change after login
        self._logged_in = True
        log.info("logged in to nemlig.com")

    async def _search_settings(self) -> dict[str, Any]:
        if self._settings and time.monotonic() - self._settings[0] < SETTINGS_TTL:
            return self._settings[1]
        app = await self._api("GET", f"{BASE}/webapi/v2/AppSettings/Website") or {}
        page = await self._api("GET", f"{BASE}/", params={"GetAsJson": "1", "d": "1"}) or {}
        page = page.get("Settings", {})
        settings = {
            "timestamp": app.get("CombinedProductsAndSitecoreTimestamp", ""),
            "timeslotUtc": page.get("TimeslotUtc", ""),
            "deliveryZoneId": page.get("DeliveryZoneId", 1),
        }
        self._settings = (time.monotonic(), settings)
        return settings

    # --- Store methods ----------------------------------------------------

    async def search(self, query: str, limit: int = 10) -> list[Product]:
        params = {"query": query, "take": limit, "skip": 0, "recipeCount": 0}
        params |= await self._search_settings()
        data = await self._api("GET", SEARCH, params=params)
        try:
            hits = data["Products"]["Products"]
        except (KeyError, TypeError) as exc:
            raise StoreError("nemlig-søgning har ændret format.") from exc
        return [self._product(h) for h in hits]

    def _product(self, hit: dict) -> Product:
        avail = hit.get("Availability") or {}
        return Product(
            store=self.name,
            id=str(hit["Id"]),
            name=hit.get("Name", ""),
            price=_money(hit.get("Price")) or Decimal(0),
            brand=hit.get("Brand") or None,
            size=hit.get("Description") or None,
            unit_price=_money(hit.get("UnitPriceCalc")),
            unit=_unit(hit.get("UnitPriceLabel")),
            ean=None,  # not in nemlig's search answers (docs/research.md)
            on_offer=bool(hit.get("Campaign") or hit.get("DiscountItem")),
            offer_text=_offer(hit.get("Campaign")),
            available=avail.get("IsDeliveryAvailable", True)
            and avail.get("IsAvailableInStock", True),
        )

    async def _basket(self) -> dict:
        return await self._api("GET", f"{BASE}/webapi/basket/GetBasket") or {}

    def _cart(self, basket: dict) -> Cart:
        lines = [
            CartLine(
                str(ln["Id"]),
                ln.get("Name", ""),
                int(ln.get("Quantity", 0)),
                _money(ln.get("Price")) or Decimal(0),
            )
            for ln in basket.get("Lines") or []
        ]
        products = _money(basket.get("TotalProductsPrice"))
        if products is None:
            products = sum((ln.line_total for ln in lines), Decimal(0))
        fees = {
            name: _money(basket.get(key))
            for name, key in (
                ("levering", "DeliveryPrice"),
                ("poser", "TotalBagsPrice"),
                ("pant", "TotalDepositsPrice"),
            )
            if basket.get(key)
        }
        total = _money(basket.get("TotalPrice"))
        warnings = [
            w.get("Message", str(w)) if isinstance(w, dict) else str(w)
            for w in basket.get("ValidationFailures") or []
        ]
        return Cart(
            store=self.name,
            lines=lines,
            products_total=products,
            fees=fees,
            total=total if total is not None else products + sum(fees.values(), Decimal(0)),
            minimum_order=_money(basket.get("MinimumOrderTotal")),
            minimum_met=basket.get("IsMinTotalValid"),
            delivery=basket.get("FormattedDeliveryTime") or None,
            warnings=warnings,
        )

    async def get_cart(self) -> Cart:
        return self._cart(await self._basket())

    async def set_quantities(self, items: dict[str, int]) -> list[QuantityResult]:
        results = []
        for product_id, quantity in items.items():
            payload = {
                "ProductId": product_id,
                "quantity": quantity,
                "AffectPartialQuantity": False,
                "disableQuantityValidation": False,
            }
            try:
                basket = (
                    await self._api("POST", f"{BASE}/webapi/basket/AddToBasket", json=payload) or {}
                )
            except StoreError as exc:
                results.append(QuantityResult(product_id, quantity, None, False, str(exc)))
                continue
            applied = next(
                (
                    int(ln.get("Quantity", 0))
                    for ln in basket.get("Lines") or []
                    if str(ln.get("Id")) == product_id
                ),
                0,
            )
            ok = applied == quantity
            message = None if ok else f"nemlig satte antallet til {applied}, ikke {quantity}."
            results.append(QuantityResult(product_id, quantity, applied, ok, message))
        return results

    async def terms(self) -> StoreTerms:
        basket = await self._basket()
        return StoreTerms(
            store=self.name,
            home_delivery=True,
            delivery_fee=_money(basket.get("DeliveryPrice")),
            other_fees={"poser": _money(basket["TotalBagsPrice"])}
            if basket.get("TotalBagsPrice")
            else {},
            minimum_order=_money(basket.get("MinimumOrderTotal")),
        )

    async def delivery_info(self) -> DeliveryInfo:
        basket = await self._basket()
        spot = await self._api("GET", f"{BASE}/webapi/Order/DeliverySpot") or {}
        slot = spot.get("TimeSlot") or {}
        chosen = basket.get("FormattedDeliveryTime")
        return DeliveryInfo(
            store=self.name,
            chosen_start=_time(slot.get("Start")),
            chosen_end=_time(slot.get("End")),
            edit_deadline=_time(spot.get("EditDeadline")),
            note=(
                f"Valgt tid i kurven: {chosen}. Gebyret afhænger af tiden (ca. 19-51 kr.)."
                if chosen
                else "Ingen tid valgt. Gebyret afhænger af tiden (ca. 19-51 kr.). Vælg på nemlig.com."
            ),
        )

    def _order(self, o: dict, lines: list[CartLine] | None = None) -> Order:
        delivery = o.get("DeliveryTime") or {}
        return Order(
            store=self.name,
            id=str(o.get("Id", "")),
            status=STATUS.get(o.get("Status"), f"status {o.get('Status')}"),
            total=_money(o.get("Total")) or Decimal(0),
            placed_at=_time(o.get("OrderDate")),
            delivery_start=_time(delivery.get("Start")),
            delivery_end=_time(delivery.get("End")),
            lines=lines or [],
        )

    async def orders(self, limit: int = 10) -> list[Order]:
        data = await self._api(
            "GET", f"{BASE}/webapi/order/GetBasicOrderHistory", params={"skip": 0, "take": limit}
        )
        return [self._order(o) for o in (data or {}).get("Orders", [])]

    async def order(self, order_id: str) -> Order:
        data = await self._api("GET", f"{BASE}/webapi/v2/order/GetOrderHistory/{order_id}") or {}
        lines = [
            CartLine(
                str(ln.get("ProductNumber", "")),
                ln.get("ProductName", ""),
                int(ln.get("Quantity", 0)),
                _money(ln.get("Amount")) or Decimal(0),
            )
            for ln in data.get("Lines") or []
        ]
        if not lines and not data.get("Id"):
            raise StoreError(f"Fandt ikke ordre {order_id} hos nemlig.")
        meta = {"Id": order_id, **{k: v for k, v in data.items() if k != "Lines"}}
        order = self._order(meta, lines)
        if not order.total:
            order.total = sum((ln.line_total for ln in lines), Decimal(0))
        return order


class _Unauthorized(Exception):
    """HTTP 401 from nemlig.com: the Bearer token or the login cookie expired."""
