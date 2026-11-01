from dataclasses import asdict
from datetime import date, datetime
from decimal import Decimal

import pytest

from handle_mcp import server
from handle_mcp import stores as registry
from handle_mcp.models import (
    Cart,
    CartLine,
    DeliveryInfo,
    Order,
    Product,
    QuantityResult,
    StoreTerms,
)

# Personal data a store adapter could leak by mistake. Every tool must remove it.
LEAK = {
    "CustomerName": "Anders And",
    "Email": "anders@example.com",
    "MobileNumber": "+45 98 76 54 32",
    "DoorCode": "kode-4711",
    "UnattendedNotes": "Nøglen ligger under måtten",
    "deliveryAddress": {"street": "Andeby 1", "zip": "postnr-9999", "city": "Andeby"},
}


class FakeStore:
    """A store that answers from memory. Smuggles LEAK into every answer."""

    name = "fake"

    def __init__(self) -> None:
        self.cart: dict[str, int] = {"1": 2}

    def _leak(self, obj):
        """Return the model as a dict with personal data mixed in, like a sloppy adapter."""
        return {**asdict(obj), **LEAK}

    async def search(self, query, limit=10):
        return [
            self._leak(
                Product(
                    "fake",
                    "1",
                    f"{query} 1 l",
                    Decimal("12.95"),
                    unit_price=Decimal("12.95"),
                    unit="l",
                    ean="5701234567890",
                )
            )
        ]

    async def get_cart(self):
        lines = [CartLine(pid, "Mælk", q, Decimal("12.95") * q) for pid, q in self.cart.items()]
        total = sum((line.line_total for line in lines), Decimal(0))
        return self._leak(
            Cart(
                "fake",
                lines,
                total,
                {"levering": Decimal(39)},
                total + 39,
                minimum_order=Decimal(500),
                minimum_met=total >= 500,
            )
        )

    async def set_quantities(self, items):
        self.cart.update(items)
        self.cart = {k: v for k, v in self.cart.items() if v}
        return [QuantityResult(pid, q, q, True) for pid, q in items.items()]

    async def delivery_info(self):
        return self._leak(
            DeliveryInfo(
                "fake",
                chosen_start=datetime(2026, 11, 3, 16),
                chosen_end=datetime(2026, 11, 3, 19),
                next_available=date(2026, 11, 2),
            )
        )

    async def terms(self):
        return StoreTerms("fake", True, Decimal(39), minimum_order=Decimal(500))

    async def orders(self, limit=10):
        return [self._leak(Order("fake", "900000000001", "Leveret", Decimal("612.50")))]

    async def order(self, order_id):
        return self._leak(
            Order(
                "fake",
                order_id,
                "Leveret",
                Decimal("612.50"),
                lines=[CartLine("1", "Mælk", 2, Decimal("25.90"))],
            )
        )


class BrokenStore(FakeStore):
    name = "broken"

    async def search(self, query, limit=10):
        raise registry.StoreError("API'et svarer 500")

    async def orders(self, limit=10):
        raise NotImplementedError


@pytest.fixture(autouse=True)
def fake_stores(monkeypatch):
    monkeypatch.setattr(registry, "STORES", {"fake": FakeStore, "broken": BrokenStore})
    monkeypatch.setattr(server, "_instances", {})
