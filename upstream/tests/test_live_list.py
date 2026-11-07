"""A fixed list of ~50 everyday items, priced live in every store.

The MCP finds and counts, the AI model picks. So an item fails only when no candidate fits:
the right product never reached the model. A wrong suggestion (or øko too dear) is a warning,
printed with "!", because the model can still pick another candidate.
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


def problems(entry: dict[str, Any], cand: dict[str, Any]) -> list[str]:
    """What is wrong with one candidate for one list entry. Empty when it fits."""
    text = f"{cand['name']} {cand.get('brand') or ''}".lower()
    out = [f"mangler '{w}'" for w in entry.get("has", []) if not _any(w, text)]
    out += [f"har '{w}'" for w in entry.get("not", []) if _any(w, text)]
    if "max_kr" in entry and Decimal(str(cand["line_total"])) > Decimal(str(entry["max_kr"])):
        out.append(f"{cand['line_total']} kr. > {entry['max_kr']} kr.")
    return out


def check(entry: dict[str, Any], line: BasketLine | None) -> tuple[list[str], list[str]]:
    """(fail, warn). Fail: no candidate fits. Warn: the suggestion is wrong or øko too dear."""
    if line is None or line.product is None:
        return ["ikke fundet"], []
    cands = [c for c in line.candidates if not (c.get("suggested") and line.guess)]
    if line.guess:
        warn = [f"gæt: {get(line.product, 'name')}"]
    else:
        first = next((c for c in line.candidates if c.get("suggested")), None)
        warn = problems(entry, first) if first else []
    if not any(not problems(entry, c) for c in cands):
        return [f"ingen kandidat passer ({len(cands)} vist)", *warn], []
    if line.oeko:
        others = [c for c in line.candidates if not c.get("suggested")]
        plain = [c["line_total"] for c in others if not c.get("oeko") and _fits(entry, c)]
        if plain and min(plain) * OEKO_MARGIN < line.line_total:
            warn.append(f"øko {line.line_total} kr. mod {min(plain)} kr.")
    return [], warn


def _fits(entry: dict[str, Any], alt: dict[str, Any]) -> bool:
    """An alternative counts for the øko rule only when it is the same kind of thing:
    not 'Citron peber' for citroner, not a tin ('Konserves') for cherrytomater."""
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


def _c(name, total="10", suggested=False, oeko=False, brand=None, category=None):
    c = {"name": name, "line_total": Decimal(total), "brand": brand, "category": category}
    return c | {"suggested": suggested, "oeko": oeko}


def _line(*cands, guess=False):
    """A BasketLine whose suggestion is the first candidate."""
    first = cands[0]
    product = Product("x", "1", first["name"], first["line_total"], first["brand"])
    cands = [first | {"suggested": True}, *cands[1:]]
    total, oeko = first["line_total"], first["oeko"]
    return BasketLine("x", 1, product, total, True, None, guess, cands, oeko)


def test_checker_finds_wrong_words_and_price():
    garlic = {"item": "hvidløg", "has": ["hvidløg"], "not": ["olie"], "max_kr": 10}
    assert problems(garlic, _c("Hvidløg 1 stk", "4")) == []
    assert problems(garlic, _c("Hvidløg i olie", "4")) == ["har 'olie'"]
    assert problems(garlic, _c("Hvidløg 3 stk", "12")) == ["12 kr. > 10 kr."]
    jam = {"item": "fior di latte", "has": ["fior di latte|mozzarella"]}
    assert problems(jam, _c("Mozzarella 125 g")) == []
    assert problems(jam, _c("Jordbær", brand="Fior di Frutta")) == [
        "mangler 'fior di latte|mozzarella'"
    ]
    cheese = {"item": "ost", "has": ["ost"], "not": ["olie"]}
    assert problems(cheese, _c("Skiveost 45+")) == []
    assert problems(cheese, _c("Leverpostej")) == ["mangler 'ost'"]
    assert problems(cheese, _c("Ost i krydderolie")) == ["har 'olie'"]
    assert problems({"item": "t", "has": ["tomat"]}, _c("Cherrytomater")) == []
    stryhn = {"item": "leverpostej", "has": ["stryhn"]}
    assert problems(stryhn, _c("Leverpostej bacon-peber", brand="Stryhn's")) == []


def test_fails_only_when_no_candidate_fits():
    corn = {"item": "majs", "has": ["majs"], "not": ["baby"]}
    assert check(corn, _line(_c("Majs 3 stk"))) == ([], [])
    # A wrong suggestion with a right candidate is a warning: the model can pick the other.
    assert check(corn, _line(_c("Babymajs"), _c("Majs 3 stk"))) == ([], ["har 'baby'"])
    assert check(corn, _line(_c("Babymajs"), _c("Babymajs øko"))) == (
        ["ingen kandidat passer (2 vist)", "har 'baby'"],
        [],
    )
    assert check(corn, None) == (["ikke fundet"], [])


def test_a_guess_never_counts_as_fitting():
    garlic = {"item": "hvidløg", "has": ["hvidløg"]}
    assert check(garlic, _line(_c("Hvidløg"), guess=True)) == (
        ["ingen kandidat passer (0 vist)", "gæt: Hvidløg"],
        [],
    )


def test_a_guess_with_a_fitting_candidate_is_a_warning():
    garlic = {"item": "hvidløg", "has": ["hvidløg"]}
    assert check(garlic, _line(_c("Løg"), _c("Hvidløg 1 stk"), guess=True)) == ([], ["gæt: Løg"])


def test_oeko_too_dear_is_a_warning():
    entry = {"item": "linser", "has": ["linser"]}
    plain = _c("Røde linser", "20")
    assert check(entry, _line(_c("Røde linser øko", "22", oeko=True), plain)) == ([], [])
    assert check(entry, _line(_c("Røde linser øko", "27", oeko=True), plain)) == (
        [],
        ["øko 27 kr. mod 20 kr."],
    )
    tin = _c("Røde linser", "9", category="Konserves")
    canned = {"item": "linser", "has": ["linser"], "not": ["konserv"]}
    assert check(canned, _line(_c("Røde linser øko", "27", oeko=True), tin)) == ([], [])


# --- live --------------------------------------------------------------------------------------


@pytest.mark.live
@pytest.mark.skipif(os.environ.get("HANDLE_LIVE") != "1", reason="set HANDLE_LIVE=1")
@pytest.mark.parametrize("name", sorted(STORES))
async def test_fixed_list_live(name):
    entries = items_for(name)
    result = await quote(STORES[name](), [e["item"] for e in entries])
    by_item = {ln.item: ln for ln in result.lines}
    failed, warned = [], 0
    print(f"\n== {name}: {len(entries)} varer, {result.products_total} kr. ==")
    for e in entries:
        line = by_item.get(e["item"])
        fail, warn = check(e, line)
        warned += bool(warn)
        mark = "✗ " if fail else "! " if warn else "? " if line and not line.sure else "  "
        pick = f"{get(line.product, 'name')[:36]:<36} {line.line_total:>7}" if line else "-"
        print(f"{mark}{e['item']:<34} → {pick} {'; '.join(fail + warn) or (line.note or '')}")
        if fail:
            failed.append(
                f"{e['item']}: {'; '.join(fail)}" + (f" ({e['why']})" if "why" in e else "")
            )
    print(f"{warned} forslag med advarsel (modellen kan vælge en anden kandidat)")
    assert not failed, f"{name}: {len(failed)} varer uden passende kandidat:\n" + "\n".join(failed)
