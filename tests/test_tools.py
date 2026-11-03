"""The tool surface: allowlist, write limits and error handling."""

import json

import pytest
from mcp.server.mcpserver.exceptions import ToolError

from handle_mcp import server

ALLOWED = {
    "search_products": False,
    "search_recipes": False,
    "get_recipe": False,
    "list_recipes": False,
    "list_offers": False,
    "compare_basket": False,
    "get_cart": False,
    "set_cart_quantities": True,
    "add_recipe_to_cart": True,
    "get_delivery_info": False,
    "list_delivery_slots": False,
    "get_orders": False,
    "get_order": False,
}


async def test_only_allowed_tools_exist():
    tools = {t.name: t for t in await server.mcp.list_tools()}
    assert set(tools) == set(ALLOWED)


async def test_only_cart_tools_write():
    for tool in await server.mcp.list_tools():
        writes = ALLOWED[tool.name]
        assert tool.annotations.read_only_hint is (not writes), tool.name
        assert tool.annotations.destructive_hint is writes, tool.name


async def test_search_one_broken_store_does_not_stop_the_others():
    result = await server.search_products("mælk")
    assert result["fake"][0]["name"] == "mælk 1 l"
    assert result["fake"][0]["price"] == "12.95"
    assert "500" in result["broken"]["error"]


async def test_search_unknown_store_is_a_clear_error():
    with pytest.raises(ToolError, match="Ukendt butik 'netto'"):
        await server.search_products("mælk", stores=["netto"])


async def test_not_implemented_answers_politely():
    with pytest.raises(ToolError, match="kan ikke det endnu"):
        await server.get_orders("broken")


async def test_set_cart_quantities_returns_results_and_new_cart():
    result = await server.set_cart_quantities("fake", {"1": 0, "2": 3})
    assert [r["applied"] for r in result["results"]] == [0, 3]
    assert [line["product_id"] for line in result["cart"]["lines"]] == ["2"]


@pytest.mark.parametrize(
    "items, match",
    [
        ({}, "Ingen varer"),
        ({"1": -1}, "Antal skal"),
        ({"1": 51}, "Antal skal"),
        ({str(i): 1 for i in range(51)}, "Højst 50"),
    ],
)
async def test_set_cart_quantities_rejects_bad_input(items, match):
    with pytest.raises(ToolError, match=match):
        await server.set_cart_quantities("fake", items)


async def test_answers_are_json():
    result = await server.get_delivery_info("fake")
    json.dumps(result)
    assert result["delivery"]["chosen_start"] == "2026-11-03T16:00:00"
    assert result["terms"]["minimum_order"] == "500"


async def test_recipes_in_a_store_without_recipes_do_not_stop_the_others(monkeypatch):
    from tests.conftest import BrokenStore

    async def no_recipes(self, query, limit=5):
        raise NotImplementedError

    monkeypatch.setattr(BrokenStore, "recipes", no_recipes)
    result = await server.search_recipes("karry")
    assert result["fake"][0]["name"] == "karry til 4"
    assert result["broken"]["error"] == "broken kan ikke det endnu."


async def test_delivery_slots_in_time_order_cheapest_first_only_free():
    fake = (await server.list_delivery_slots())["fake"]
    assert [s["price"] for s in fake["slots"]] == ["25", "19", "0"]  # sold-out slot left out
    assert [s["price"] for s in fake["cheapest"]] == ["0", "19", "25"]
    assert fake["cheapest"][0]["start"] == "2026-11-04T14:00:00"
