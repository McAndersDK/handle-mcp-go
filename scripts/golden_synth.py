"""The synthetic corpus and the item list for the match goldens.

Products are the ones the tests build by hand (tests/test_compare.py), plus a few edge cases.
Items come from tests/live_list.json, WEEK_LIST, every item literal in tests/test_compare.py
(found with ast, so nothing is retyped here) and a list of adversarial strings.
"""

from __future__ import annotations

import ast
import json
from datetime import date
from decimal import Decimal

from handle_mcp.models import Product
from scripts.golden_io import ROOT
from tests.test_compare import BILKA, NEMLIG, WEEK_LIST


def p(pid, name, price, size=None, available=True, brand=None, category=None, **extra) -> Product:
    product = Product("x", pid, name, Decimal(price), brand, size, available=available)
    product.category = category
    for key, value in extra.items():
        setattr(product, key, value)
    return product


def synthetic() -> list[Product]:
    pal = "Mejeri & køl > Pålæg > Kødpålæg > Kyllingepålæg > Kyllingebryst i skiver"
    raw = "Kød & fisk > Kylling > Kyllingebryst & kyllingefilet > Kyllingebrystfilet"
    laks = "Kød & fisk > Fisk & skaldyr > Laks > Laksestykke"
    tins = "Kolonial > Konserves, bouillon & krydderier > Konserveret frugt & grønt > Majs"
    extra = [
        p("s1", "Røget kyllingebryst", "11.00", "100 g", category=pal),
        p("s2", "Kyllingebryst i skiver", "32.00", "350 g", category=pal),
        p("s3", "Kyllingebrystfilet", "32.95", "280 g", category=raw),
        p("s4", "Kyllingebrystfilet", "64.95", "650 g", category=raw),
        p("s5", "Kyllingebrystfilet", "239.00", "4 kg", category="Frost > Kylling"),
        p("s6", "Kyllingebrystfilet øko", "69.00", "225 g", category=raw),
        p("s7", "Laksestykke", "109.95", "600 g", brand="Skagen food", category=laks),
        p("s8", "Laksestykke", "59.00", "300 g", available=False, category=laks),
        p("s9", "Indbagt laksefilet m. spinat", "19.00", "250 g", category="Frost > Indbagt laks"),
        p("s10", "Majs", "12.25", "3 x 70 g", category=tins),
        p("s11", "Babymajs", "15.00", "125 g", category="Frugt & grønt > Grøntsager > Minimajs"),
        p("s12", "Hakket oksekød m. 50% grønt", "30.00", "500 g"),
        p("s13", "Hakket oksekød 14-18%", "47.44", "500 g"),
        p("s14", "Minimælk med 0,5% fedt", "10.00", "1 l"),
        p("s15", "Blåbær", "18.00", "125 g", category="Grønt"),
        p("s16", "Blåbær", "29.50", "300 g / frost", category="Frost"),
        p("s17", "Mini frugtstænger m blåbær øko", "9.45", "7 x 12,5 g", category="Kolonial"),
        p("s18", "Cherrytomater i tomatjuice", "8.00", "400 g", category="Kolonial > Konserves"),
        p("s19", "Cherry tomater øko", "17.00", "250 g", category="Frugt & grønt > Tomater"),
        p("s20", "Hvidløg i krydderolie", "17.80", "290 g"),
        p("s21", "Lagerøl", "8.00", "33 cl"),
        p("s22", "Classic toiletpapir", "32.00", "12 rl."),
        p("s23", "Aloe Vera fugtigt toiletpapir", "22.95", "42 stk."),
        p("s24", "Køkkenrulle 3-lags", "20.00", "4 rl."),
        p("s25", "Fior di Frutta marmelade", "24.00", "300 g", brand="Fior di Frutta"),
        p("s26", "Skyr m. skovbær 0,2%", "30.95", "1 kg", brand="Cheasy"),
        p("s27", "Appetit agurker", "29.5", "570 g / 350 g / i skiver", category="Kolonial"),
        p("s28", "Agurk øko.", "12.0", "1 stk.", category="Grønt"),
        p("s29", "Kakaomælk", "8.00", "1 l"),
        p("s30", "Mælk", "12.00", "1 l"),
        p("s31", "Post-it", "10.00", category="Mejeri > Ost"),
        p("s32", "Salt & peber", "15.00"),
        p("s33", "Kyllingeoverlår", "60.00", "800 g"),
        p("s34", "Frilandsæg", "25.00", "10 stk"),
        p("s35", "Æg økologiske", "41.95", "10 stk"),
        p("s36", "Bananer", "2.75", "1 stk"),
        p("s37", "Bananer 4 pak øko", "10.00", "4 stk"),
        p("s38", "Pærer", "1.00"),
        p("s39", "Spaghettisquash øko.", "25.0", "1 stk.", category="Grønt"),
        p("s40", "Spaghetti øko.", "7.95", "500 g / Casaneo", category="Kolonial"),
        p("s41", "Fior di latte", "20.00", "200 g"),
        p("s42", "Øko fior di latte", "22.00", "100 g", offer_text="3 for 48 kr"),
        p("s43", "Kyllingelår", "113.50", "600 g", offer_text="2 for 175 kr"),
        p(
            "s44",
            "Laksefilet",
            "84.53",
            "600 g",
            before_price=Decimal("120.75"),
            offer_until=date(2026, 11, 4),
            offer_text="-30 %",
        ),
        p(
            "s45",
            "Mums rosinboller",
            "0",
            "6 stk",
            before_price=Decimal("20.45"),
            max_per_order=1,
            offer_text="Gratis (max 1 stk.)",
        ),
        p("s46", "Bananer", "2.75", "1 stk", offer_text="Mix 10 for 25,00", max_per_order=10),
        p("s47", "Hakkede tomater", "13.95", "400 g", offer_text="4 for 50 kr"),
        p(
            "s48",
            "Hakkede tomater",
            "13.95",
            "400 g",
            offer_text="4 for 50,5 kr",
            offer_until=date(2026, 11, 3),
            before_price=Decimal("13.95"),
        ),
        p("s49", "Æbler", "12.00", "1,5 kg"),
        p("s50", "Tomater", "0.125", "0,5 kg"),
    ]
    return [*NEMLIG, *BILKA, *extra]


ADVERSARIAL = [
    "2 × letmælk 1 l",
    "2×letmælk",
    "2 x letmælk 1 l",
    "2\u00a0x\u00a0letmælk 1\u00a0l",
    "2\u00a0×\u00a0letmælk",
    "6\u00a0x\u00a0bananer",
    "hakket oksekød 1,5\u00a0kg",
    "12 × 0,25 l cola",
    "12\u00a0×\u00a00,25\u00a0l cola",
    "12×0,25 l cola",
    "12x0,25 l cola",
    "\u00a02 x mælk",
    "3 x 2 x mælk",
    "0 x mælk",
    "00 x mælk",
    "2 x",
    "x",
    "",
    "   ",
    "1,5 kg æbler",
    "1.5 kg æbler",
    "kaffe 1 KG",
    "ÆG 10 STK",
    "Øko æg 10 stk",
    "ÅRHUS",
    "Kg mel",
    "1 Kg mel",
    "٢ x mælk",
    "mælk ١ l",
    "1l mælk",
    "1 ltr mælk",
    "50 cl øl",
    "5 dl fløde",
    "100 gr smør",
    "toiletpapir 8 rl",
    "2 x 8 ruller toiletpapir",
    "mælk\n1 l",
    "mælk\t1 l",
    "makrel med tomat",
    "yoghurt m banan",
    "uden sukker",
    "1 kg",
    "kg",
    "1,5,5 kg",
    "1.000 g mel",
    "01 kg",
    "99999999999999999999 g mel",
    "99999999999999999999 x mælk",
    "İs",
    "Fløde 38%",
    "mælk 0,5% fedt",
    "x2 mælk",
    "mælk  1  l",
    "MÆLK",
    "Mælk, 1 l",
    "kyllingebryst - 450 g",
    "kyllingebryst 450g",
    "kyllingebryst 0,45 kg",
    "kyllingebryst 4,5 dl",
    "hakket oksekød 8-12%",
    "e-vitamin",
    "æbler/pærer",
    "ris 1 kg ris",
]

NAMES = {"best_match", "brief", "parse_item", "parse_size", "query"}


def test_compare_items() -> list[str]:
    """Every item string the tests pass to best_match, brief, parse_item, parse_size, query or
    compare_basket([...])."""
    tree = ast.parse((ROOT / "upstream" / "tests" / "test_compare.py").read_text(encoding="utf-8"))
    found: list[str] = []
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call):
            continue
        func = (
            node.func.attr if isinstance(node.func, ast.Attribute) else getattr(node.func, "id", "")
        )
        for arg in node.args[:1]:
            if func in NAMES and isinstance(arg, ast.Constant) and isinstance(arg.value, str):
                found.append(arg.value)
            if func == "compare_basket" and isinstance(arg, ast.List):
                found += [e.value for e in arg.elts if isinstance(e, ast.Constant)]
    return found


def items() -> list[str]:
    live = json.loads((ROOT / "tests" / "live_list.json").read_text(encoding="utf-8"))
    every = [i["item"] for i in live["items"]] + WEEK_LIST + test_compare_items() + ADVERSARIAL
    seen: dict[str, None] = {}
    for item in every:
        if isinstance(item, str):
            seen.setdefault(item)
    return list(seen)
