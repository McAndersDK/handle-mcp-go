"""The stores. A new store = one file here + one line in STORES.

Every store is a class with the async methods in `Store`. If a store cannot do
something, the method raises NotImplementedError and the tool says so politely.
Store methods may only *read*, except `set_quantities`. Ordering, paying,
choosing a delivery time and changing the address must never exist here.
"""

from __future__ import annotations

import json
import os
import stat
from pathlib import Path
from typing import Protocol

from handle_mcp.models import Cart, DeliveryInfo, Order, Product, QuantityResult, StoreTerms

CONFIG_DIR = Path(os.environ.get("HANDLE_CONFIG_DIR", "~/.config/handle-mcp")).expanduser()


class StoreError(Exception):
    """A clear, user-facing failure from one store (login failed, API changed, ...)."""


class Store(Protocol):
    name: str

    async def search(self, query: str, limit: int = 10) -> list[Product]: ...
    async def get_cart(self) -> Cart: ...
    async def set_quantities(self, items: dict[str, int]) -> list[QuantityResult]: ...
    async def delivery_info(self) -> DeliveryInfo: ...
    async def terms(self) -> StoreTerms: ...
    async def orders(self, limit: int = 10) -> list[Order]: ...
    async def order(self, order_id: str) -> Order: ...


def load_login(store: str) -> dict[str, str]:
    """Read a store's login from env vars or ~/.config/handle-mcp/<store>.json.

    Env vars win: HANDLE_<STORE>_EMAIL and HANDLE_<STORE>_PASSWORD.
    The file must be readable by its owner only (chmod 600).
    """
    prefix = f"HANDLE_{store.upper()}_"
    email, password = os.environ.get(prefix + "EMAIL"), os.environ.get(prefix + "PASSWORD")
    if email and password:
        return {"email": email, "password": password}

    path = CONFIG_DIR / f"{store}.json"
    if not path.exists():
        raise StoreError(
            f"Intet login til {store}. Sæt {prefix}EMAIL og {prefix}PASSWORD, "
            f"eller opret {path} med chmod 600."
        )
    if path.stat().st_mode & (stat.S_IRWXG | stat.S_IRWXO):
        raise StoreError(f"{path} kan læses af andre. Kør: chmod 600 {path}")
    data = json.loads(path.read_text())
    if not data.get("email") or not data.get("password"):
        raise StoreError(f"{path} mangler 'email' eller 'password'.")
    return {"email": data["email"], "password": data["password"]}


from handle_mcp.stores.bilkatogo import BilkaStore
from handle_mcp.stores.nemlig import NemligStore

STORES: dict[str, type[Store]] = {
    "nemlig": NemligStore,
    "bilkatogo": BilkaStore,
}
