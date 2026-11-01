"""compare_basket with a real week list against two fake stores. No network."""

from datetime import date
from decimal import Decimal
from typing import ClassVar

import pytest

from handle_mcp import compare, server
from handle_mcp import stores as registry
from handle_mcp.compare import _score, best_match, parse_item, parse_size
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
    assert result["stores"]["nemlig"]["notes"] == [
        "Under minimumskøb: 278.00 kr. mangler til 300 kr."
    ]


def test_singular_and_plural_match():
    line = best_match("bananer", [p("x", "b", "Banan øko.", "3.00", "1 stk")])
    assert line.product.id == "b" and line.sure  # singular and plural are the same product


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


def test_flavour_word_is_not_the_product():
    """Seen live: Bilka 'banan' matched 'Yoghurt m. banan' as sure."""
    yoghurt = p("x", "y", "Yoghurt m. banan", "12.00")
    line = best_match("14 x banan", [yoghurt, p("x", "b", "Bananer", "2.75", "1 stk")])
    assert line.product.id == "b"


def test_flavour_only_hit_is_never_a_guess():
    """Seen live: nemlig 'mandariner' matched 'Grøn te m. mandarin & appelsin'."""
    line = best_match("mandariner 1 kg", [p("x", "t", "Grøn te m. mandarin & appelsin", "31.50")])
    assert line.product is None and line.note == "Ikke fundet"


def test_store_category_breaks_a_tie_on_the_name():
    """Seen live: Bilka 'laks fersk' matched a nail polish."""
    polish = p("x", "n", "Neglelak laks", "29.00")
    polish.category = "Personlig pleje > Makeup > Neglelak"
    salmon = p("x", "l", "Laks", "85.00", "600 g")
    salmon.category = "Kød & fisk > Fisk > Laks"
    assert best_match("laks fersk", [polish, salmon]).product.id == "l"


def test_name_score_beats_the_category():
    coffee = p("x", "k", "Kaffe", "45.00")
    coffee.category = "Kolonial > Kaffe"
    filters = p("x", "f", "Kaffe filter", "20.00")
    filters.category = "Husholdning > Filtre"
    assert best_match("kaffe filter", [coffee, filters]).product.id == "f"


def test_partial_name_hit_is_still_a_guess():
    line = best_match("kaffe filter brun", [p("x", "f", "Kaffefilter", "20.00")])
    assert line.product.id == "f" and line.guess


def test_category_fits_a_compound_word():
    fresh = p("x", "f", "Cherrytomater", "15.00", "250 g")
    fresh.category = "Frugt & grønt > Grøntsager > Tomater"
    juice = p("x", "j", "Cherrytomater", "10.95", "400 g")
    juice.category = "Kolonial > Konserves > Dåsemad"
    assert best_match("cherrytomater", [juice, fresh]).product.id == "f"


def test_and_joins_two_products_not_a_flavour():
    line = best_match("peber", [p("x", "s", "Salt & peber", "15.00")])
    assert line.product.id == "s" and line.sure


def test_short_category_word_is_not_a_compound_hit():
    cheese = p("x", "o", "Post-it", "10.00")
    cheese.category = "Mejeri > Ost"
    assert compare._category_rank(["post"], cheese) == 1


def test_relaxed_search_hit_is_marked_unsure():
    """Bilka retries with optional words: 'cheasy skyr passionsfrugt' can return another flavour."""
    other = p("x", "s", "Skyr m. skovbær 0,2%", "30.95", "1 kg", brand="Cheasy")
    line = best_match("4 x cheasy skyr passionsfrugt", [other])
    assert line.product.id == "s" and not line.sure and "Tjek varen" in line.note


def test_relaxed_search_hit_without_brand_is_only_a_guess():
    line = best_match("cheasy skyr passionsfrugt", [p("x", "n", "Skyr naturel", "20.00", "1 kg")])
    assert line.product.id == "n" and line.guess and not line.sure


def test_size_in_the_item_is_the_amount_wanted():
    line = best_match("hakket oksekød 1,5 kg", [p("x", "s", "Hakket oksekød", "35.00", "400 g")])
    assert (line.quantity, line.line_total, line.sure) == (4, Decimal("140.00"), True)


def test_cheapest_for_the_amount_wins():
    small = p("x", "s", "Laks", "30.00", "2 x 90 g")
    big = p("x", "b", "Laks", "89.00", "600 g")
    line = best_match("laks 600 g", [small, big])  # 4 small packs cost 120
    assert (line.product.id, line.quantity, line.line_total) == ("b", 1, Decimal("89.00"))


def test_quantity_without_size_picks_the_cheapest_line():
    bag = p("x", "bag", "Pærer", "22.00", "1 kg")
    one = p("x", "one", "Pære", "2.75", "1 stk")
    line = best_match("4 x pærer", [bag, one])
    assert (line.product.id, line.line_total) == ("one", Decimal("11.00"))


def test_cheapest_wins_on_the_same_name():
    eco = p("x", "eco", "Æg økologiske", "41.95", "10 stk")
    cheap = p("x", "cheap", "Æg frilands", "25.00", "10 stk")
    assert best_match("æg 10 stk", [eco, cheap]).product.id == "cheap"


def test_compound_word_counts_but_whole_word_wins():
    assert _score(["mælk"], p("x", "k", "Kakaomælk", "8.00")) == 0.75
    whole = p("x", "m", "Mælk", "12.00", "1 l")
    cocoa = p("x", "k", "Kakaomælk", "8.00", "1 l")
    assert best_match("mælk 1 l", [cocoa, whole]).product.id == "m"


def test_much_bigger_pack_is_marked():
    line = best_match("kaffe 400 g", [p("x", "k", "Kaffe", "45.00", "500 g")])
    assert not line.sure and "Anden størrelse" in line.note


def test_whole_word_beats_cheaper_compound_word():
    whole = p("x", "w", "Ris", "20.00", "1 kg")
    compound = p("x", "c", "Kokosris", "10.00", "1 kg")
    assert best_match("ris 1 kg", [compound, whole]).product.id == "w"


def test_how_it_is_made_is_the_same_product():
    eco = p("x", "eco", "Økologiske æg S", "13.95", "4 stk")
    offer = p("x", "offer", "Frilandsæg", "25.00", "10 stk")
    line = best_match("æg 10 stk", [eco, offer])  # 3 small packs would cost 41.85
    assert (line.product.id, line.line_total, line.sure) == ("offer", Decimal("25.00"), True)


def test_multi_buy_counts_in_the_line_total():
    legs = p("x", "l", "Kyllingelår", "113.50", "600 g")
    legs.offer_text = "2 for 175 kr"
    line = best_match("kyllingelår 1,2 kg", [legs])
    assert (line.quantity, line.line_total) == (2, Decimal(175))


def test_offer_that_ends_before_delivery_does_not_count():
    salmon = p("x", "s", "Laksefilet", "84.53", "600 g")
    salmon.before_price, salmon.offer_until = Decimal("120.75"), date(2026, 11, 4)
    line = best_match("laksefilet 600 g", [salmon], on=date(2026, 11, 9))
    assert line.line_total == Decimal("120.75") and "slutter 04/11" in line.note
    assert best_match("laksefilet 600 g", [salmon], on=date(2026, 11, 3)).line_total == Decimal(
        "84.53"
    )


def test_fresh_beats_cheaper_frozen_or_canned():
    frozen = p("x", "f", "Blåbær frost", "20.00", "300 g")
    fresh = p("x", "b", "Blåbær", "25.00", "125 g")
    assert best_match("blåbær", [frozen, fresh]).product.id == "b"
    canned = p("x", "c", "Cherrytomater i tomatjuice", "8.00", "400 g")
    canned.category = "Kolonial > Konserves"
    tomato = p("x", "t", "Cherrytomater", "15.00", "250 g")
    assert best_match("cherrytomater", [canned, tomato]).product.id == "t"
    assert best_match("frosne blåbær", [frozen, fresh]).product.id == "f"


def test_frozen_meat_offer_still_wins():
    frozen = p("x", "f", "Hele kyllingelår m. rygben frost", "35.00", "1250 g")
    frozen.category = "Frost > Kød og fjerkræ"
    fresh = p("x", "k", "Hele kyllingelår", "64.95", "1.6 kg")
    assert best_match("kyllingelår 1,25 kg", [fresh, frozen]).product.id == "f"


def test_canned_by_name_or_category_loses():
    canned = p("x", "c", "Cherrytomater i tomatjuice", "10.95", "400 g")
    canned.category = "Kolonial > Konserves & dåsemad"
    fresh = p("x", "t", "Cherrytomater", "12.00", "400 g")
    assert best_match("cherrytomater 250 g", [canned, fresh]).product.id == "t"


def test_c_and_k_and_short_plural():
    assert best_match("klementiner", [p("x", "c", "Clementiner", "20.00")]).sure
    assert best_match("peberfrugt rød", [p("x", "p", "Peberfrugter røde", "20.00")]).sure


def test_canned_loses_even_when_fresh_has_no_fitting_category():
    """Seen live at Bilka: the canned one sits in '... > Cherrytomater', the fresh one does not."""
    canned = p("x", "c", "Cherrytomater i tomatjuice", "10.95", "400 g")
    canned.category = "Kolonial > Konserves, bouillon & krydderier > Tomatprodukter > Cherrytomater"
    fresh = p("x", "f", "Cherrytomater", "12.00", "400 g")
    fresh.category = "Frugt & grønt > Grønt"
    assert best_match("cherrytomater 250 g", [canned, fresh]).product.id == "f"


def test_split_compound_is_the_same_word():
    """Seen live at Bilka: every 'Cherrytomater' is canned; the fresh one is 'Cherry tomater øko'."""
    canned = p("x", "c", "Cherrytomater", "12.00", "400 g")
    canned.category = "Kolonial > Konserves > Flåede tomater"
    fresh = p("x", "f", "Cherry tomater øko", "17.00", "250 g")
    fresh.category = "Frugt & grønt > Grøntsager > Tomater > Cherrytomater"
    line = best_match("cherrytomater 250 g", [canned, fresh])
    assert line.product.id == "f" and line.sure


def test_same_cost_more_food_wins():
    small = p("x", "s", "Snackpeber mini", "22.00", "250 g")
    big = p("x", "b", "Snackpeber", "22.00", "500 g")
    assert best_match("snackpeber", [small, big]).product.id == "b"
    assert best_match("snackpeber", [big, small]).product.id == "b"


def test_delivery_day_is_danish():
    from datetime import UTC, datetime

    info = DeliveryInfo("x", chosen_start=datetime(2026, 11, 4, 23, 30, tzinfo=UTC))
    assert compare._day(info) == date(2026, 11, 5)  # 00:30 in Denmark


def test_blend_loses_to_plain_and_is_not_sure():
    """Seen live at nemlig: 'Hakket oksekød m. 50% grønt' was a sure match for minced beef."""
    blend = p("x", "g", "Hakket oksekød m. 50% grønt", "30.00", "500 g")
    plain = p("x", "o", "Hakket oksekød 14-18%", "47.44", "500 g")
    assert best_match("hakket oksekød 500 g", [blend, plain]).product.id == "o"
    only = best_match("hakket oksekød 500 g", [blend])
    assert not only.sure and "Blandingsvare" in only.note
    assert best_match("rugbrød", [p("x", "r", "Rugbrød m. kerner", "22.00", "1 kg")]).sure


def test_no_size_compares_per_kg_not_per_pack():
    """Seen live at Bilka: 'æbler' gave 1 apple for 2.75 while nemlig gave 1 kg."""
    one = p("x", "1", "Æbler", "2.75", "1 stk")
    bag = p("x", "b", "Æbler", "12.00", "1 kg")
    line = best_match("æbler", [one, bag])
    assert line.product.id == "b" and "Skriv en mængde" in line.note
    six = p("x", "6", "Toiletpapir", "20.00", "6 rl.")
    eight = p("x", "8", "Toiletpapir", "15.22", "8 rl.")
    assert best_match("toiletpapir", [six, eight]).product.id == "8"


def test_guess_with_a_size_is_the_cheapest_of_that_size():
    """Seen live at nemlig: 'pasta' only finds fresh pasta, and Tortelloni 250 g was the guess."""
    tortelloni = p("x", "t", "Tortelloni m. ricotta", "25.00", "250 g")
    fettuccine = p("x", "f", "Fettuccine", "13.95", "500 g")
    line = best_match("pasta 500 g", [tortelloni, fettuccine])
    assert (line.product.id, line.guess, line.sure) == ("f", True, False)
    assert best_match("pasta 500 g", [p("x", "s", "Penne", "9.00", "6 stk")]).product is None


def test_fat_share_or_size_is_not_a_blend():
    for name in ("Minimælk med 0,5% fedt", "Hakket svinekød u. 8% fedt", "Smør i 250 g"):
        assert compare._mixed(["x"], p("x", "a", name, "10.00")) == 0
    assert compare._mixed(["oksekød"], p("x", "b", "Oksekød m. 50% grønt", "10.00")) == 1


def test_count_without_size_prefers_pieces_and_unknown_size_comes_last():
    bag = p("x", "bag", "Pærer", "22.00", "1 kg")
    one = p("x", "one", "Pære", "2.75", "1 stk")
    loose = p("x", "loose", "Pærer", "1.00")  # no size at all
    assert best_match("4 x pærer", [loose, bag, one]).product.id == "one"
    assert best_match("pærer", [loose, one, bag]).product.id == "bag"


async def test_no_minimum_note_for_an_empty_basket(two_stores):
    result = await server.compare_basket(["findes ikke xyz"])
    assert not any("minimumskøb" in n for n in result["stores"]["nemlig"]["notes"])


def test_count_of_pieces_buys_enough_packs_not_n_packs():
    """Seen live at Bilka: '6 x bananer' gave 6 four-packs = 24 bananas."""
    four = p("x", "4", "Bananer", "10.00", "4 stk")
    line = best_match("6 x bananer", [four])
    assert (line.quantity, line.line_total) == (2, Decimal("20.00"))


def test_never_less_than_wanted():
    """Seen live: 3 x 300 g for 'hakket kylling 1 kg', 3 x 180 g for 'laks 600 g'."""
    small = p("x", "s", "Hakket kylling", "59.00", "300 g")
    assert best_match("hakket kylling 1 kg", [small]).quantity == 4
    assert best_match("mælk 1 l", [p("x", "m", "Mælk", "10.00", "990 ml")]).quantity == 1


def test_alternatives_let_the_caller_swap():
    shallot = p("x", "s", "Skalotteløg banan", "15.00", "250 g")
    banana = p("x", "b", "Bananer", "3.00", "1 stk")
    cheap = p("x", "c", "Bananer øko", "2.50", "1 stk")
    line = best_match("bananer", [shallot, banana, cheap])
    ids = [a["id"] for a in line.alternatives]
    assert line.product.id not in ids and len(ids) == 2
    assert {"id", "name", "size", "quantity", "line_total", "offer_text", "note"} <= set(
        line.alternatives[0]
    )


def test_alternatives_skip_store_guesses():
    banana = p("x", "b", "Bananer", "3.00", "1 stk")
    tea = p("x", "t", "Grøn te", "10.00", "100 g")
    assert best_match("bananer", [banana, tea]).alternatives == []


def test_alternatives_keep_the_note():
    small = p("x", "s", "Hakket kylling", "20.00", "300 g")
    big = p("x", "b", "Hakket kylling", "59.00", "1 kg")
    line = best_match("hakket kylling 1 kg", [small, big])
    assert line.product.id == "b"
    assert line.alternatives[0]["note"]


def test_alternatives_survive_out():
    banana = p("x", "b", "Bananer", "3.00", "1 stk")
    cheap = p("x", "c", "Bananer øko", "2.50", "1 stk")
    out = server._out(best_match("bananer", [banana, cheap]))
    assert out["alternatives"][0]["line_total"] in {"3.00", "2.50"}
