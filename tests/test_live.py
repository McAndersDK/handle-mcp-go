"""Read-only smoke tests against the real stores.

Run: HANDLE_LIVE=1 uv run pytest -m live -s
Needs logins in env vars or ~/.config/handle-mcp/<store>.json.
These tests NEVER change the cart.
"""

import os

import pytest

from handle_mcp.stores import STORES

pytestmark = [
    pytest.mark.live,
    pytest.mark.skipif(os.environ.get("HANDLE_LIVE") != "1", reason="set HANDLE_LIVE=1"),
]


@pytest.fixture(params=sorted(STORES))
def store(request):
    return STORES[request.param]()


async def test_search(store):
    products = await store.search("minimælk", limit=3)
    assert products and all(p.price > 0 for p in products)


async def test_cart_terms_delivery(store):
    cart = await store.get_cart()
    assert cart.total >= 0
    await store.terms()
    await store.delivery_info()


async def test_orders(store):
    orders = await store.orders(limit=2)
    if orders:
        await store.order(orders[0].id)
