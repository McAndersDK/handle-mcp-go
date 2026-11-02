"""A fixed list of ~50 everyday items, priced live in every store.

A matcher change that picks the wrong product shows up here before the family shops.
The list is data: tests/live_list.json. The offline tests below check the list and the checker.

Run: HANDLE_LIVE=1 uv run pytest -m live -s tests/test_live_list.py
Needs logins (keychain or env). Only reads: never changes the cart.
"""

import json
import os
import re
from decimal import Decimal
from pathlib import Path
from typing import Any

import pytest

from handle_mcp.compare import BasketLine, quote
from handle_mcp.match import get, parse_item
from handle_mcp.models import Product
from handle_mcp.stores import STORES

LIST = json.loads((Path(__file__).parent / "live_list.json").read_text(encoding="utf-8"))["items"]
KEYS = {"item", "has", "not", "stores", "max_kr", "why"}
OEKO_MARGIN = Decimal("1.10")  # øko only when it costs at most 10 % more


def items_for(store: str) -> list[dict[str, Any]]:
    return [e for e in LIST if store in e.get("stores", STORES)]


def problems(entry: dict[str, Any], line: BasketLine | None) -> list[str]:
    """What is wrong with the pick for one list entry. Empty when the pick is fine."""
    if line is None or line.product is None:
        return ["ikke fundet"]
    if line.guess:
        return [f"gæt: {get(line.product, 'name')}"]
    text = f"{get(line.product, 'name')} {get(line.product, 'brand') or ''}".lower()
    out = [f"mangler '{w}'" for w in entry.get("has", []) if not _any(w, text)]
    out += [f"har '{w}'" for w in entry.get("not", []) if _any(w, text)]
    if "max_kr" in entry and line.line_total > Decimal(str(entry["max_kr"])):
        out.append(f"{line.line_total} kr. > {entry['max_kr']} kr.")
    if line.oeko:
        plain = [a["line_total"] for a in line.alternatives if not a["oeko"] and _fits(entry, a)]
        if plain and min(plain) * OEKO_MARGIN < line.line_total:
            out.append(f"øko {line.line_total} kr. mod {min(plain)} kr.")
    return out


def _fits(entry: dict[str, Any], alt: dict[str, Any]) -> bool:
    """An alternative counts for the øko rule only when it is the same kind of thing:
    not 'Citron peber' for citroner, not a tin ('Konserves') for cherrytomater."""
    # Needs #38 (brand and category on alternatives). Without it, brand words never match here.
    text = f"{alt['name']} {alt.get('brand') or ''} {alt.get('category') or ''}".lower()
    return all(_any(w, text) for w in entry.get("has", [])) and not any(
        _any(w, text) for w in entry.get("not", [])
    )


def _any(word: str, text: str) -> bool:
    """'a|b' means a or b. A word must start or end a name word, with a Danish plural ending:
    'kylling' hits 'Kyllingebryst', 'olie' hits 'krydderolie', 'tomat' hits 'Cherrytomater'.
    A word deep inside another does not: 'ost' is not 'Leverpostej', 'ris' is not 'friske'."""
    return any(
        re.search(rf"\b{re.escape(w)}|{re.escape(w)}(?:e|er|r|s)?\b", text)
        for w in word.lower().split("|")
    )


# --- offline: the list and the checker ---------------------------------------------------------


def test_list_is_about_50_unique_items():
    assert 45 <= len(LIST) <= 60
    names = [e["item"] for e in LIST]
    assert len(names) == len(set(names))
    for e in LIST:
        assert set(e) <= KEYS, e
        assert parse_item(e["item"])[1], e  # the store gets words to search for
        assert e.get("has"), e
        assert set(e.get("stores", [])) <= {"nemlig", "bilkatogo"}, e
    assert len(items_for("nemlig")) >= 45 and len(items_for("bilkatogo")) >= 45


def _line(name, total="10", oeko=False, guess=False, alternatives=(), brand=None):
    product = Product("x", "1", name, Decimal(total), brand)
    return BasketLine(name, 1, product, Decimal(total), True, None, guess, list(alternatives), oeko)


def test_checker_finds_wrong_words_price_and_guess():
    garlic = {"item": "hvidløg", "has": ["hvidløg"], "not": ["olie"], "max_kr": 10}
    assert problems(garlic, _line("Hvidløg 1 stk", "4")) == []
    assert problems(garlic, _line("Hvidløg i olie", "4")) == ["har 'olie'"]
    assert problems(garlic, _line("Hvidløg 3 stk", "12")) == ["12 kr. > 10 kr."]
    assert problems(garlic, _line("Hvidløg", guess=True)) == ["gæt: Hvidløg"]
    assert problems(garlic, None) == ["ikke fundet"]
    jam = {"item": "fior di latte", "has": ["fior di latte|mozzarella"]}
    assert problems(jam, _line("Mozzarella 125 g")) == []
    assert problems(jam, _line("Jordbær", brand="Fior di Frutta")) == [
        "mangler 'fior di latte|mozzarella'"
    ]
    cheese = {"item": "ost", "has": ["ost"], "not": ["olie"]}
    assert problems(cheese, _line("Skiveost 45+")) == []
    assert problems(cheese, _line("Leverpostej")) == ["mangler 'ost'"]
    assert problems(cheese, _line("Ost i krydderolie")) == ["har 'olie'"]
    assert problems({"item": "t", "has": ["tomat"]}, _line("Cherrytomater")) == []
    stryhn = {"item": "leverpostej", "has": ["stryhn"]}
    assert problems(stryhn, _line("Leverpostej bacon-peber", brand="Stryhn's")) == []


def test_checker_oeko_within_10_percent():
    alt = [{"name": "Røde linser", "line_total": Decimal(20), "oeko": False}]
    entry = {"item": "linser", "has": ["linser"]}
    assert problems(entry, _line("Røde linser øko", "22", oeko=True, alternatives=alt)) == []
    assert problems(entry, _line("Røde linser øko", "27", oeko=True, alternatives=alt)) == [
        "øko 27 kr. mod 20 kr."
    ]
    tin = [
        {"name": "Røde linser", "category": "Konserves", "line_total": Decimal(9), "oeko": False}
    ]
    canned = {"item": "linser", "has": ["linser"], "not": ["konserv"]}
    assert problems(canned, _line("Røde linser øko", "27", oeko=True, alternatives=tin)) == []


# --- live --------------------------------------------------------------------------------------


@pytest.mark.live
@pytest.mark.skipif(os.environ.get("HANDLE_LIVE") != "1", reason="set HANDLE_LIVE=1")
@pytest.mark.parametrize("name", sorted(STORES))
async def test_fixed_list_live(name):
    entries = items_for(name)
    result = await quote(STORES[name](), [e["item"] for e in entries])
    by_item = {ln.item: ln for ln in result.lines}
    failed = []
    print(f"\n== {name}: {len(entries)} varer, {result.products_total} kr. ==")
    for e in entries:
        line = by_item.get(e["item"])
        bad = problems(e, line)
        mark = "✗ " if bad else "? " if line and not line.sure else "  "
        pick = f"{get(line.product, 'name')[:36]:<36} {line.line_total:>7}" if line else "-"
        print(f"{mark}{e['item']:<34} → {pick} {'; '.join(bad) or (line.note or '')}")
        if bad:
            failed.append(
                f"{e['item']}: {'; '.join(bad)}" + (f" ({e['why']})" if "why" in e else "")
            )
    assert not failed, f"{name}: {len(failed)} forkerte valg:\n" + "\n".join(failed)
