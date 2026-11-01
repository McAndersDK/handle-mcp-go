"""compare_basket with a real week list against two fake stores. No network."""

from decimal import Decimal
from typing import ClassVar

import pytest

from handle_mcp import compare, server
from handle_mcp import stores as registry
from handle_mcp.compare import best_match, parse_item, parse_size
from handle_mcp.models import DeliveryInfo, Product, StoreTerms

WEEK_LIST = [
    "2 x letmælk 1 l",
    "rugbrød",
    "æg 10 stk",
    "smør 250 g",
    "hakket oksekød 500 g",
    "kyllingebryst",
    "bananer",
    "æbler",
    "kartofler 2 kg",
    "gulerødder 1 kg",
    "pasta 500 g",
    "havregryn 1 kg",
    "kaffe 400 g",
    "toiletpapir",
    "skyr 1 kg",
]


def p(store, pid, name, price, size=None, available=True, brand=None):
    return Product(store, pid, name, Decimal(price), brand, size, available=available)


NEMLIG = [
    p("nemlig", "n1", "Letmælk 0,5% fedt", "11.95", "1 l"),
    p("nemlig", "n2", "Kakaomælk", "9.95", "1 l"),
    p("nemlig", "n3", "Rugbrød m. kerner", "22.00", "1 kg", brand="Schulstad"),
    p("nemlig", "n4", "Økologiske æg", "34.95", "10 stk"),
    p("nemlig", "n5", "Smør", "24.95", "250 g", brand="Lurpak"),
    p("nemlig", "n6", "Hakket oksekød 8-12%", "45.00", "500 g"),
    p("nemlig", "n7", "Kyllingebryst", "59.00", "900 g"),
    p("nemlig", "n8", "Bananer", "3.00", "1 stk"),
    p("nemlig", "n9", "Æbler Pink Lady", "25.00", "1 kg"),
    p("nemlig", "n10", "Kartofler", "20.00", "2 kg"),
    p("nemlig", "n11", "Gulerødder", "12.00", "1 kg"),
    p("nemlig", "n12", "Pasta penne", "14.00", "500 g"),
    p("nemlig", "n13", "Havregryn", "16.00", "1 kg"),
    p("nemlig", "n14", "Kaffe formalet", "45.00", "500 g"),  # only 500 g
    p("nemlig", "n15", "Toiletpapir 8 ruller", "39.00", "8 stk"),
    p("nemlig", "n16", "Skyr vanilje", "30.00", "1 kg"),
]
BILKA = [
    p("bilkatogo", "b1", "Letmælk", "10.50", "1 l"),
    p("bilkatogo", "b2", "Letmælk", "21.00", "2 l"),
    p("bilkatogo", "b3", "Rugbrød", "18.00", "1 kg"),
    p("bilkatogo", "b4", "Æg fra fritgående høns", "29.00", "10 stk"),
    p("bilkatogo", "b5", "Smør", "22.00", "250 g"),
    p("bilkatogo", "b6", "Hakket oksekød 8-12%", "40.00", "500 g"),
    p("bilkatogo", "b7", "Kyllingebrystfilet", "55.00", "900 g"),  # word inside a word
    p("bilkatogo", "b8", "Bananer", "2.50", "1 stk"),
    p("bilkatogo", "b9", "Æbler", "22.00", "1 kg"),
    p("bilkatogo", "b10", "Kartofler", "18.00", "2 kg"),
    p("bilkatogo", "b11", "Gulerødder", "10.00", "1 kg"),
    p("bilkatogo", "b12", "Pasta fusilli", "12.00", "500 g"),
    p("bilkatogo", "b13", "Havregryn", "15.00", "1 kg"),
    p("bilkatogo", "b14", "Kaffe", "42.00", "400 g"),
    p("bilkatogo", "b15", "Toiletpapir", "35.00", "8 stk", available=False),  # sold out
    p("bilkatogo", "b16", "Skyr naturel", "28.00", "1 kg"),
]


class CatalogStore:
    """Searches a list like a store: any product whose name shares a word with the query."""

    name = "catalog"
    catalog: ClassVar[list[Product]] = []
    terms_ = StoreTerms("catalog", True)
    note: str | None = None

    def __init__(self) -> None:
        self.searches: list[str] = []

    async def search(self, query, limit=10):
        self.searches.append(query)
        words = [w for w in parse_item(query)[1] if len(w) > 1]
        found = [x for x in self.catalog if any(w[:5] in x.name.lower() for w in words)]
        return found[:limit]

    async def terms(self):
        return self.terms_

    async def delivery_info(self):
        return DeliveryInfo(self.terms_.store, note=self.note)


class Nemlig(CatalogStore):
    catalog = NEMLIG
    terms_ = StoreTerms("nemlig", True, Decimal(29), {"poser": Decimal(6)}, Decimal(300))
    note = "Leveringsgebyret afhænger af den valgte tid (19-51 kr.)."


class Bilka(CatalogStore):
    catalog = BILKA
    terms_ = StoreTerms("bilkatogo", True, Decimal(39), {"pakkeservice": Decimal(39)}, Decimal(500))


@pytest.fixture
def two_stores(monkeypatch):
    monkeypatch.setattr(registry, "STORES", {"nemlig": Nemlig, "bilkatogo": Bilka})


def test_parse_size_and_item():
    assert parse_size("1 l") == ("ml", Decimal(1000))
    assert parse_size("12 x 0,25 l / Classic") == ("ml", Decimal("3000.00"))
    assert parse_size("500 g") == ("g", Decimal(500))
    assert parse_size("Rugbrød") is None
    assert parse_item("2 x letmælk 1 l") == (2, ["letmælk"], ("ml", Decimal(1000)))
    assert parse_item("æg 10 stk") == (1, ["æg"], ("stk", Decimal(10)))
    assert parse_item("hakket oksekød med fedt") == (1, ["hakket", "oksekød", "fedt"], None)


def test_whole_word_beats_word_inside_word():
    line = best_match("mælk 1 l", [NEMLIG[1], p("x", "m", "Mælk", "12.00", "1 l")])
    assert line.product.id == "m" and line.sure  # not the cheaper Kakaomælk


def test_right_size_beats_cheaper_wrong_size():
    line = best_match("letmælk 2 l", BILKA[:2])
    assert line.product.id == "b2" and line.sure


def test_wrong_size_is_kept_but_marked():
    line = best_match("kaffe 400 g", NEMLIG)
    assert line.product.id == "n14" and not line.sure
    assert "Anden størrelse" in line.note


def test_sold_out_is_missing():
    line = best_match("toiletpapir", [BILKA[14]])  # the only hit is sold out
    assert line.product is None and line.note == "Ikke fundet"


async def test_week_list_in_both_stores(two_stores):
    result = await server.compare_basket(WEEK_LIST)
    nemlig, bilka = result["stores"]["nemlig"], result["stores"]["bilkatogo"]

    assert nemlig["missing"] == []
    assert bilka["missing"] == ["toiletpapir"]
    milk = nemlig["lines"][0]
    assert (milk["product"]["id"], milk["quantity"], milk["line_total"]) == ("n1", 2, "23.90")
    unsure = {ln["item"]: ln["note"] for ln in bilka["lines"] if not ln["sure"]}
    assert set(unsure) == {"kyllingebryst"}  # Kyllingebrystfilet: word inside a word
    assert [ln["item"] for ln in nemlig["lines"] if not ln["sure"]] == ["kaffe 400 g"]

    assert nemlig["fees"] == {"levering": "29", "poser": "6"}
    assert nemlig["products_total"] == "413.80"
    assert nemlig["total"] == "448.80"
    assert nemlig["minimum_met"] is True
    assert nemlig["notes"] == [Nemlig.note]
    assert bilka["minimum_met"] is False  # 334.50 kr. < 500 kr.

    # Compared on the 14 items both stores have, plus fees.
    assert result["comparable_totals"] == {"nemlig": "409.80", "bilkatogo": "412.50"}
    assert result["cheapest"] == "nemlig"  # Bilka is cheaper on goods but has higher fees
    assert "Tjek dem" in result["summary"]


async def test_one_broken_store_does_not_stop_the_compare(monkeypatch):
    monkeypatch.setattr(
        registry, "STORES", {"nemlig": Nemlig, "bilkatogo": Bilka, "broken": BrokenCatalog}
    )
    result = await server.compare_basket(["rugbrød"])
    assert "500" in result["stores"]["broken"]["error"]
    assert result["cheapest"] == "nemlig"  # 22 + 35 kr. fees beats 18 + 78 kr.
    assert "Fejl: broken." in result["summary"]


async def test_no_winner_when_only_one_store_answers(monkeypatch):
    monkeypatch.setattr(registry, "STORES", {"nemlig": Nemlig, "broken": BrokenCatalog})
    result = await server.compare_basket(["rugbrød"])
    assert result["cheapest"] is None
    assert result["summary"] == "Kun nemlig kunne prise listen. Fejl: broken."


class BrokenCatalog(CatalogStore):
    async def search(self, query, limit=10):
        raise registry.StoreError("API'et svarer 500")


async def test_not_home_delivery_is_noted(monkeypatch):
    class Pickup(Bilka):
        terms_ = StoreTerms("bilkatogo", False)

    monkeypatch.setattr(registry, "STORES", {"bilkatogo": Pickup})
    result = await server.compare_basket(["rugbrød"])
    assert result["stores"]["bilkatogo"]["notes"] == ["Kurven er ikke sat til hjemlevering."]


async def test_empty_or_too_long_list_is_refused(two_stores):
    from mcp.server.mcpserver.exceptions import ToolError

    with pytest.raises(ToolError, match="tom"):
        await server.compare_basket([" ", ""])
    with pytest.raises(ToolError, match="Højst"):
        await server.compare_basket(["mælk"] * (server.MAX_ITEMS + 1))


async def test_search_gets_words_only(two_stores):
    """Store search engines miss '2 x letmælk 1 l'; quantity and size are for matching only."""
    store = server._store("bilkatogo")
    await server.compare_basket(["2 x letmælk 1 l", "hakket oksekød 500 g", "æg 10 stk"])
    assert store.searches == ["letmælk", "hakket oksekød", "æg"]


async def test_compare_only_reads(two_stores):
    """compare_basket must never touch the cart: the fakes have no cart methods at all."""
    assert not hasattr(Nemlig, "set_quantities")
    await server.compare_basket(WEEK_LIST[:3])


def test_verdict_with_no_store():
    assert compare.verdict({"x": {"error": "nede"}})["cheapest"] is None


def test_verdict_without_common_items_names_no_winner():
    a = compare.BasketQuote(
        "a", [], ["x"], Decimal(0), {"levering": Decimal(19)}, Decimal(19), None, None
    )
    b = compare.BasketQuote(
        "b", [], ["x"], Decimal(0), {"levering": Decimal(39)}, Decimal(39), None, None
    )
    assert compare.verdict({"a": a, "b": b})["cheapest"] is None


async def test_failing_delivery_info_keeps_the_prices(monkeypatch):
    class NoDelivery(Nemlig):
        async def delivery_info(self):
            raise NotImplementedError

    monkeypatch.setattr(registry, "STORES", {"nemlig": NoDelivery})
    result = await server.compare_basket(["rugbrød"])
    assert result["stores"]["nemlig"]["lines"][0]["product"]["id"] == "n3"
    assert result["stores"]["nemlig"]["notes"] == []


def test_singular_and_plural_match():
    line = best_match("bananer", [p("x", "b", "Banan øko.", "3.00", "1 stk")])
    assert line.product.id == "b" and not line.sure


def test_store_top_hit_when_no_name_matches():
    """Seen live: nemlig answers 'pasta' with 'Penne rigate'."""
    sold_out = p("x", "s", "Spaghetti", "9.00", "500 g", available=False)
    line = best_match("pasta 500 g", [sold_out, p("x", "pe", "Penne rigate", "12.00", "500 g")])
    assert line.product.id == "pe" and not line.sure
    assert "bedste søgeresultat" in line.note


def test_guesses_stay_out_of_the_verdict():
    good = compare.best_match("rugbrød", [p("a", "r", "Rugbrød", "20.00")])
    guess = compare.best_match("pasta", [p("b", "pe", "Penne", "1.00")])
    assert guess.guess
    quotes = {
        n: compare.BasketQuote(n, lines, [], Decimal(0), {}, Decimal(0), None, None)
        for n, lines in {"a": [good, good], "b": [good, guess]}.items()
    }
    quotes["a"].lines[1] = compare.BasketLine("pasta", 1, good.product, Decimal(50), True)
    verdict = compare.verdict(quotes)
    assert verdict["comparable_totals"] == {"a": Decimal(20), "b": Decimal(20)}  # pasta left out
