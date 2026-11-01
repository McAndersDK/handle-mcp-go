"""The MCP server: every tool, store selection and error handling.

stdout is the JSON-RPC stream. All logging goes to stderr.
Every answer goes through privacy.scrub() in `_out()`.
"""

from __future__ import annotations

import asyncio
import dataclasses
import logging
import sys
from datetime import date, datetime
from decimal import Decimal
from typing import Any

from mcp.server.mcpserver import MCPServer
from mcp.server.mcpserver.exceptions import ToolError
from mcp.types import ToolAnnotations

from handle_mcp import stores as registry
from handle_mcp.privacy import scrub

log = logging.getLogger("handle_mcp")

MAX_ITEMS = 50  # per set_cart_quantities call
MAX_QUANTITY = 50  # per product
MAX_RESULTS = 25

READ = ToolAnnotations(readOnlyHint=True, destructiveHint=False, openWorldHint=True)
WRITE = ToolAnnotations(
    readOnlyHint=False, destructiveHint=True, idempotentHint=True, openWorldHint=True
)

mcp = MCPServer(
    "handle",
    instructions=(
        "Tools for Danish grocery stores with home delivery. You can search, compare "
        "baskets and change cart quantities. You can never order or pay: the human "
        "always does that on the store's website."
    ),
)

_instances: dict[str, registry.Store] = {}


def _store(name: str) -> registry.Store:
    if name not in registry.STORES:
        known = ", ".join(sorted(registry.STORES)) or "ingen endnu"
        raise ToolError(f"Ukendt butik '{name}'. Kendte butikker: {known}.")
    if name not in _instances:
        _instances[name] = registry.STORES[name]()
    return _instances[name]


def _names(requested: list[str] | None) -> list[str]:
    names = requested or sorted(registry.STORES)
    if not names:
        raise ToolError("Ingen butikker er sat op endnu.")
    for name in names:
        _store(name)  # fail early on an unknown name
    return names


def _jsonable(value: Any) -> Any:
    if dataclasses.is_dataclass(value) and not isinstance(value, type):
        return _jsonable(dataclasses.asdict(value))
    if isinstance(value, dict):
        return {k: _jsonable(v) for k, v in value.items()}
    if isinstance(value, list | tuple):
        return [_jsonable(v) for v in value]
    if isinstance(value, Decimal):
        return str(value)
    if isinstance(value, datetime | date):
        return value.isoformat()
    return value


def _out(value: Any) -> dict[str, Any]:
    result = scrub(_jsonable(value))
    return result if isinstance(result, dict) else {"result": result}


def _error_text(store: str, exc: BaseException) -> str:
    if isinstance(exc, NotImplementedError):
        return f"{store} kan ikke det endnu."
    if isinstance(exc, registry.StoreError):
        return f"{store}: {exc}"
    log.exception("%s failed", store, exc_info=exc)
    return f"{store}: uventet fejl ({type(exc).__name__}). Se serverens log."


async def _call(store: str, method: str, *args: Any) -> Any:
    try:
        return await getattr(_store(store), method)(*args)
    except ToolError:
        raise
    except Exception as exc:
        raise ToolError(_error_text(store, exc)) from exc


async def _each(names: list[str], method: str, *args: Any) -> dict[str, Any]:
    """Call every store at once. One store failing never stops the others."""
    results = await asyncio.gather(
        *(getattr(_store(n), method)(*args) for n in names), return_exceptions=True
    )
    return {
        name: {"error": _error_text(name, r)} if isinstance(r, BaseException) else r
        for name, r in zip(names, results, strict=True)
    }


@mcp.tool(annotations=READ)
async def search_products(query: str, stores: list[str] | None = None, limit: int = 10) -> dict:
    """Search products in one or more stores. Returns price, price per kg/l, offers and EAN when known."""
    limit = max(1, min(limit, MAX_RESULTS))
    return _out(await _each(_names(stores), "search", query, limit))


@mcp.tool(annotations=READ)
async def compare_basket(items: list[str], stores: list[str] | None = None) -> dict:
    """Price a shopping list in each store: best match per item, total incl. fees, minimum order met, unsure and missing matches marked."""
    raise ToolError("compare_basket kommer i trin 5.")


@mcp.tool(annotations=READ)
async def get_cart(store: str) -> dict:
    """Show the cart in one store: lines, fees, total and whether the minimum order is met."""
    return _out(await _call(store, "get_cart"))


@mcp.tool(annotations=WRITE)
async def set_cart_quantities(store: str, items: dict[str, int]) -> dict:
    """Set ABSOLUTE quantities in one store's cart (0 removes the product). Never orders or pays. Returns a result per product and the new cart."""
    if not items:
        raise ToolError("Ingen varer angivet.")
    if len(items) > MAX_ITEMS:
        raise ToolError(f"Højst {MAX_ITEMS} varer pr. kald.")
    bad = [pid for pid, qty in items.items() if not 0 <= qty <= MAX_QUANTITY]
    if bad:
        raise ToolError(f"Antal skal være 0-{MAX_QUANTITY}. Forkert for: {', '.join(bad)}.")
    results = await _call(store, "set_quantities", dict(items))
    return _out({"results": results, "cart": await _call(store, "get_cart")})


@mcp.tool(annotations=READ)
async def get_delivery_info(store: str) -> dict:
    """Show the chosen delivery time, edit deadline and next free day, plus fees and minimum order."""
    info, terms = await asyncio.gather(_call(store, "delivery_info"), _call(store, "terms"))
    return _out({"delivery": info, "terms": terms})


@mcp.tool(annotations=READ)
async def get_orders(store: str, limit: int = 10) -> dict:
    """List recent orders in one store (newest first). Use get_order for the lines."""
    limit = max(1, min(limit, MAX_RESULTS))
    return _out({"orders": await _call(store, "orders", limit)})


@mcp.tool(annotations=READ)
async def get_order(store: str, order_id: str) -> dict:
    """Show one order with what was bought."""
    return _out(await _call(store, "order", order_id))


def main() -> None:
    logging.basicConfig(stream=sys.stderr, level=logging.INFO)
    mcp.run("stdio")


if __name__ == "__main__":
    main()
