"""recipecart goldens: plan() for skip, swap, include and the error cases, plus in_cart and report."""

from __future__ import annotations

from dataclasses import replace
from decimal import Decimal

from handle_mcp.models import CartLine, QuantityResult, RecipeItem
from handle_mcp.privacy import jsonable
from handle_mcp.recipe_cart import PlanError, in_cart, plan, report
from scripts.golden_io import dump
from tests.test_recipe_cart import KEBAB, _p

ALT = RecipeItem(
    "Hakket", _p("1", "8-12%"), alternatives=[_p("2", "4-7%", available=False), _p("3", "x", 3)]
)
TWICE = replace(
    KEBAB,
    items=[
        RecipeItem("Løg", _p("10", "Løg", 2)),
        RecipeItem("Rødløg", _p("10", "Løg", 3)),
        RecipeItem("Salt", _p("11", "Salt"), have_at_home=True),
    ],
)

CASES = {
    "default": (KEBAB, {}),
    "skip_by_name": (KEBAB, {"skip": ["agurk"]}),
    "skip_by_name_with_spaces_and_case": (KEBAB, {"skip": ["  HAKKET GRISE- OG KALVEKØD "]}),
    "skip_by_id": (KEBAB, {"skip": ["106593", "2301108"]}),
    "skip_have_at_home": (KEBAB, {"skip": ["Salt"]}),
    "skip_sold_out": (KEBAB, {"skip": ["Iceberg"]}),
    "swap_by_id": (KEBAB, {"swap": {"5026536": "5062387"}}),
    "swap_by_name": (KEBAB, {"swap": {"hakket grise- og kalvekød": "5062387"}}),
    "swap_int_like_id": (KEBAB, {"swap": {"5026536": " 5062387"}}),
    "include_have_at_home": (KEBAB, {"include": ["Olivenolie"]}),
    "include_by_id": (KEBAB, {"include": ["5602504", "5049714"]}),
    "include_normal_item": (KEBAB, {"include": ["Agurk"]}),
    "skip_swap_include": (
        KEBAB,
        {"skip": ["agurk"], "swap": {"5026536": "5062387"}, "include": ["Olivenolie"]},
    ),
    "skip_and_swap_same_item": (
        KEBAB,
        {"skip": ["Hakket grise- og kalvekød"], "swap": {"5026536": "5062387"}},
    ),
    "swap_to_sold_out_alternative": (replace(KEBAB, items=[ALT]), {"swap": {"Hakket": "2"}}),
    "swap_to_second_alternative": (replace(KEBAB, items=[ALT]), {"swap": {"Hakket": "3"}}),
    "same_product_twice_adds_up": (TWICE, {}),
    "only_have_at_home": (replace(KEBAB, items=KEBAB.items[-2:]), {}),
    "no_items": (replace(KEBAB, items=[]), {}),
    "none_arguments": (KEBAB, {"skip": None, "swap": None, "include": None}),
    "error_skip_unknown": (KEBAB, {"skip": ["Pizza"]}),
    "error_include_unknown": (KEBAB, {"include": ["Peber"]}),
    "error_swap_unknown_ingredient": (KEBAB, {"swap": {"Pizza": "1"}}),
    "error_swap_not_an_alternative": (KEBAB, {"swap": {"Rødløg": "999"}}),
    "error_swap_item_without_alternatives_by_id": (KEBAB, {"swap": {"106593": "5026536"}}),
    "error_first_unknown_wins": (KEBAB, {"skip": ["agurk", "Pizza"], "include": ["Peber"]}),
    "error_empty_key": (KEBAB, {"skip": [""]}),
}


def plan_case(recipe, kwargs: dict) -> dict:
    try:
        lines, skipped = plan(recipe, **kwargs)
    except PlanError as exc:
        return {"error": {"type": "PlanError", "message": str(exc)}}
    return {"lines": lines, "skipped": skipped}


def export() -> dict[str, int]:
    cases = [
        {"name": name, "recipe": jsonable(recipe), "args": kwargs, **plan_case(recipe, kwargs)}
        for name, (recipe, kwargs) in CASES.items()
    ]
    lines, _ = plan(KEBAB)
    cart = {
        "lines": [CartLine("2301108", "Rødløg", 3, Decimal(30)), CartLine(5, "x", 1, Decimal(0))]
    }
    results = [
        QuantityResult("2301108", 4, 4, True),
        QuantityResult("5026536", 2, 1, False, "kun 1"),
    ]
    rest = {
        "in_cart": {"cart": jsonable(cart), "output": in_cart(cart)},
        "in_cart_empty": {"cart": {}, "output": in_cart({})},
        "in_cart_lines_null": {"cart": {"lines": None}, "output": in_cart({"lines": None})},
        "report": {
            "lines": lines,
            "before": {"2301108": 3},
            "results": jsonable(results),
            "output": report(lines, {"2301108": 3}, results),
        },
        "report_no_results": {"lines": lines, "before": {}, "output": report(lines, {}, [])},
    }
    return {"recipecart/plan.json": dump("recipecart/plan.json", {"cases": cases, **rest})}
