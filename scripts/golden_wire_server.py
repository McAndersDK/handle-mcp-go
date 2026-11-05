"""The MCP server for the wire goldens: the real handle_mcp server with fake stores, over stdio.

Run as a subprocess by golden_wire: `python -m scripts.golden_wire_server clean|probe`.
It gets the same guards as the export, in its own process, and no network. stdout is the
JSON-RPC stream, so nothing here prints.

clean: FakeStore and BrokenStore from tests/conftest, but `_leak` returns the object unchanged
       and slots are plain DeliverySlots (no LeakySlot).
probe: the clean store, plus methods that put the arguments they got into the answer.
"""

from __future__ import annotations

import sys

from scripts import golden_guard

loop = golden_guard.install()  # before handle_mcp is imported

import anyio

from scripts.golden_guard import memory_keyring

memory_keyring()
golden_guard.assert_safe()

from datetime import datetime
from decimal import Decimal

from handle_mcp import server
from handle_mcp import stores as registry
from handle_mcp.models import (
    DeliverySlot,
    Order,
    Product,
    QuantityResult,
    RecipeDetails,
)
from tests.conftest import BrokenStore, FakeStore


class CleanFake(FakeStore):
    def _leak(self, obj):
        return obj

    async def delivery_slots(self, days=7):
        tue, wed = datetime(2026, 11, 3, 14), datetime(2026, 11, 4, 14)
        return [
            DeliverySlot("fake", tue, tue.replace(hour=21), Decimal(25), kind="Fleksibel"),
            DeliverySlot("fake", tue, tue.replace(hour=21), Decimal(19), kind="Personlig"),
            DeliverySlot("fake", wed, wed.replace(hour=21), Decimal(0), kind="Personlig"),
            DeliverySlot("fake", wed, wed.replace(hour=17), Decimal(0), available=False),
        ]


class CleanBroken(BrokenStore, CleanFake):
    name = "broken"


class Probe(CleanFake):
    """Every int and list the tools pass on shows in the answer."""

    async def orders(self, limit=10):
        return [Order("fake", f"limit={limit!r}", "Leveret", Decimal(0))]

    async def recipe(self, recipe_id, persons=None):
        return RecipeDetails("fake", recipe_id, f"persons={persons!r}", "https://example.com", 4)

    async def delivery_slots(self, days=7):
        base = (await super().delivery_slots(days))[1]
        return [DeliverySlot("fake", base.start, base.end, Decimal(0), note=f"days={days!r}")]

    async def set_quantities(self, items):
        return [QuantityResult(pid, q, q, True, f"qty={q!r}") for pid, q in items.items()]

    async def search(self, query, limit=10):
        return [Product("fake", "1", f"q={query!r} limit={limit!r}", Decimal("12.95"))]


def main() -> None:
    mode = sys.argv[1]
    registry.STORES = {"fake": Probe if mode == "probe" else CleanFake, "broken": CleanBroken}
    server._instances = {}
    server.configure_logging()
    options = {"loop_factory": lambda: loop}  # the loop made before the socket guard
    anyio.run(server.mcp.run_stdio_async, backend="asyncio", backend_options=options)


if __name__ == "__main__":
    main()
