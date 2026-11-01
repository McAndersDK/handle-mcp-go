"""The few shapes every store returns. Money is Decimal DKK, never float or øre."""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import date, datetime
from decimal import Decimal


@dataclass
class Product:
    store: str
    id: str  # the store's own product id, used in the cart
    name: str
    price: Decimal
    brand: str | None = None
    size: str | None = None  # as the store writes it, e.g. "1 l" or "400 g"
    unit_price: Decimal | None = None  # price per unit below
    unit: str | None = None  # "kg", "l" or "stk"
    ean: str | None = None
    on_offer: bool = False
    offer_text: str | None = None
    available: bool = True
    category: str | None = None  # the store's category path, e.g. "Frugt & grønt > Bananer"


@dataclass
class CartLine:
    product_id: str
    name: str
    quantity: int
    line_total: Decimal


@dataclass
class Cart:
    store: str
    lines: list[CartLine]
    products_total: Decimal
    fees: dict[str, Decimal]  # e.g. {"levering": 39, "pakning": 39}
    total: Decimal
    minimum_order: Decimal | None = None
    minimum_met: bool | None = None
    delivery: str | None = None  # chosen delivery time as text, if any
    warnings: list[str] = field(default_factory=list)


@dataclass
class QuantityResult:
    product_id: str
    requested: int
    applied: int | None  # what the store actually set; None if unknown
    ok: bool
    message: str | None = None


@dataclass
class StoreTerms:
    store: str
    home_delivery: bool
    delivery_fee: Decimal | None = None
    other_fees: dict[str, Decimal] = field(default_factory=dict)
    minimum_order: Decimal | None = None
    source: str = "api"  # "api" or "config"


@dataclass
class DeliveryInfo:
    store: str
    chosen_start: datetime | None = None
    chosen_end: datetime | None = None
    edit_deadline: datetime | None = None
    next_available: date | None = None
    note: str | None = None


@dataclass
class Order:
    store: str
    id: str
    status: str
    total: Decimal
    placed_at: datetime | None = None
    delivery_start: datetime | None = None
    delivery_end: datetime | None = None
    lines: list[CartLine] = field(default_factory=list)  # empty in order lists
