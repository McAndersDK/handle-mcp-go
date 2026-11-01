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
    terms = await store.terms()
    info = await store.delivery_info()
    # Printed so a human can check the facts (no personal data in these fields).
    print(f"\n[{store.name}] hjemlevering={terms.home_delivery} gebyrer={cart.fees}")
    print(
        f"[{store.name}] minimum={cart.minimum_order} nået={cart.minimum_met} advarsler={cart.warnings}"
    )
    print(
        f"[{store.name}] valgt tid={info.chosen_start}-{info.chosen_end} næste ledige={info.next_available}"
    )


async def test_orders(store):
    orders = await store.orders(limit=2)
    if orders:
        await store.order(orders[0].id)
