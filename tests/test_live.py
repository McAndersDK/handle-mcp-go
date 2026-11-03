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


async def test_delivery_slots(store):
    """GET only: lists slots, never chooses one. Printed for a human check of the prices."""
    slots = [s for s in await store.delivery_slots(days=3) if s.available]
    assert slots and all(s.end > s.start for s in slots)
    for s in sorted(slots, key=lambda s: s.start)[:12]:
        print(f"\n[{store.name}] {s.start:%a %d/%m %H}-{s.end:%H} {s.kind or ''} {s.price} kr")


async def test_nemlig_recipes():
    recipes = await STORES["nemlig"]().recipes("kylling karry", limit=3)
    for r in recipes:
        print(f"\n[nemlig] {r.name} | {r.url}")
    assert recipes and all(r.url.startswith("https://www.nemlig.com/") for r in recipes)


async def test_nemlig_featured_recipe_has_products():
    nemlig = STORES["nemlig"]()
    (first, *_) = await nemlig.featured_recipes(limit=1)
    recipe = await nemlig.recipe(first.url, persons=6)
    print(f"\n[nemlig] {first.group}: {recipe.name} | {recipe.persons} pers. | {recipe.total} kr")
    for item in recipe.items:
        p = item.product
        print(
            f"  {'(har du)' if item.have_at_home else ''} {item.ingredient}: {p.name} x{p.quantity}"
        )
    assert recipe.persons == 6 and recipe.total > 0
    assert any(not i.have_at_home for i in recipe.items)


async def test_nemlig_recipes_by_tag():
    recipes = await STORES["nemlig"]().recipes_by_tag(["Familievenlig", "Under 30 min"], limit=5)
    for r in recipes:
        print(f"\n[nemlig] {r.group}: {r.name} | {r.url}")
    assert recipes and all(r.url.startswith("https://www.nemlig.com/maaltider/") for r in recipes)


async def test_orders(store):
    orders = await store.orders(limit=2)
    if orders:
        await store.order(orders[0].id)


async def test_compare_week_list(monkeypatch):
    """Prices the 15-item week list in every store. Prints a table for a human check."""
    from handle_mcp import server
    from handle_mcp import stores as registry
    from tests.test_compare import WEEK_LIST

    monkeypatch.setattr(registry, "STORES", STORES)  # conftest swaps in fakes
    monkeypatch.setattr(server, "_instances", {})
    result = await server.compare_basket(WEEK_LIST)
    for name, quote in result["stores"].items():
        print(f"\n== {name} ==")
        if "error" in quote:
            print(quote["error"])
            continue
        for ln in quote["lines"]:
            prod = ln["product"]
            mark = "  " if ln["sure"] else "? "
            print(
                f"{mark}{ln['item']:<22} → {prod['name'][:38]:<38} {prod['size'] or '':<10}"
                f" {ln['line_total']:>7} kr. {ln['note'] or ''}"
            )
        for item in quote["missing"]:
            print(f"✗ {item:<22} → ikke fundet")
        print(
            f"varer={quote['products_total']} gebyrer={quote['fees']} total={quote['total']}"
            f" minimum nået={quote['minimum_met']} noter={quote['notes']}"
        )
    print(f"\n{result['summary']}")
    assert result["cheapest"]
