"""nemlig recipes: real answers from nemlig's productbff API (fixtures, 2026-11-03)."""

from decimal import Decimal

import pytest

from handle_mcp import server
from handle_mcp.stores import StoreError
from handle_mcp.stores.nemlig_recipes import recipe_slug
from tests import test_nemlig

fake, store = test_nemlig.fake, test_nemlig.store  # the fixtures with nemlig's fake HTTP

KEBAB = "https://www.nemlig.com/maaltider/opskrift/kebabfad-med-fladbrod-98001303"


async def test_recipe_has_the_products_to_buy(store):
    r = await store.recipe(KEBAB, 4)
    assert (r.id, r.persons, r.min_persons, r.max_persons) == (
        "kebabfad-med-fladbrod-98001303",
        4,
        2,
        12,
    )
    assert (r.time, r.url, len(r.ingredients)) == ("40 min", KEBAB, 17)
    buy = [i for i in r.items if not i.have_at_home]
    home = [i.ingredient for i in r.items if i.have_at_home]
    assert len(buy) == 10
    assert home == ["Neutral olie", "Salt", "Peber", "Olivenolie", "Sukker"]
    meat = next(i for i in buy if i.ingredient == "Hakket grise- og kalvekød")
    assert (meat.product.id, meat.product.quantity, meat.product.price) == (
        "5026536",
        1,
        Decimal("29.95"),
    )
    assert meat.alternatives and meat.alternatives[0].id != meat.product.id
    assert r.total == Decimal("155.40")  # the 10 products, not the have-at-home ones


async def test_recipe_sends_slug_and_persons(store, fake):
    await store.recipe("/opskrifter/kebabfad-med-fladbrod-98001303", 6)
    dialog = next(r for r in fake.requests if r.url.path.endswith("/recipeProductDialog"))
    assert dialog.url.params["recipeId"] == "kebabfad-med-fladbrod-98001303"
    assert dialog.url.params["servings"] == "6"


@pytest.mark.parametrize("bad", ["kebab%2f..", "kebab fad", ""])
def test_recipe_id_must_be_a_slug(bad):
    with pytest.raises(StoreError, match="Kender ikke opskriften"):
        recipe_slug(bad)


async def test_recipe_format_change_is_a_clear_error(store, monkeypatch):
    async def empty(*_args, **_kwargs):
        return {}

    monkeypatch.setattr(store, "_api", empty)
    with pytest.raises(StoreError, match="ændret format"):
        await store.recipe(KEBAB)


async def test_featured_recipes_are_grouped(store):
    recipes = await store.featured_recipes(20)
    assert len(recipes) == 16
    assert recipes[1].name == "Kebabfad med fladbrød og hvidløgsdressing"
    assert recipes[1].group == "Mad i fad" and recipes[1].url == KEBAB
    assert len(await store.featured_recipes(3)) == 3


async def test_queued_recipe_api_is_a_clear_error(store, fake):
    fake.api_queue = True
    with pytest.raises(StoreError, match="kø"):
        await store.featured_recipes()


async def test_get_recipe_tool_clamps_persons(monkeypatch):
    seen = {}

    async def recipe(self, recipe_id, persons=None):
        seen["persons"] = persons
        return {"id": recipe_id}

    monkeypatch.setattr("tests.conftest.FakeStore.recipe", recipe)
    await server.get_recipe("fake", "r1", 99)
    assert seen["persons"] == server.MAX_PERSONS
    await server.get_recipe("fake", "r1", 1)
    assert seen["persons"] == server.MIN_PERSONS


async def test_recipe_without_price_or_servings(store, monkeypatch):
    row = {"ingredientTitle": "Løg", "item": {"product": {"id": "1", "title": "Løg"}}}

    async def api(_method, url, **_kwargs):
        return {"includedProducts": [row]} if url.endswith("/recipeProductDialog") else {}

    monkeypatch.setattr(store, "_api", api)
    r = await store.recipe(KEBAB, 6)
    assert (r.persons, r.items[0].product.price, r.total) == (6, Decimal(0), Decimal(0))
