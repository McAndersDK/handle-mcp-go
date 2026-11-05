"""compare goldens: best_match per corpus, quote, verdict and the search sort modes.

Answers go through server._out(), the same exit a tool answer takes (jsonable then scrub).
The three message templates with money are normalised when the file is written (golden_io).
"""

from __future__ import annotations

from datetime import UTC, date, datetime, timedelta, timezone
from decimal import Decimal
from zoneinfo import ZoneInfo

from handle_mcp import compare, server
from handle_mcp import stores as registry
from handle_mcp.match import asked, parse_item, score
from handle_mcp.models import DeliveryInfo, Product, StoreTerms
from scripts.golden_corpora import corpora, groups
from scripts.golden_io import dump
from scripts.golden_match import DAYS, day_key
from scripts.golden_synth import items as all_items
from tests.test_compare import WEEK_LIST, Bilka, CatalogStore, Nemlig

DK = ZoneInfo("Europe/Copenhagen")
TERMS_N = StoreTerms("nemlig", True, Decimal(29), {"poser": Decimal(6)}, Decimal(300))
TERMS_B = StoreTerms("bilkatogo", True, Decimal(39), {"pakkeservice": Decimal(39)}, Decimal(500))
PICKUP = StoreTerms("bilkatogo", False)


def _info(**kw) -> DeliveryInfo:
    return DeliveryInfo("x", **kw)


DELIVERY = {
    "none": _info(),
    "chosen_naive": _info(chosen_start=datetime(2026, 11, 4, 16)),
    "chosen_utc_after_midnight_in_dk": _info(
        chosen_start=datetime(2026, 11, 4, 23, 30, tzinfo=UTC)
    ),
    "chosen_dk_summer": _info(chosen_start=datetime(2026, 7, 1, 0, 30, tzinfo=DK)),
    "chosen_plus9": _info(
        chosen_start=datetime(2026, 11, 4, 20, tzinfo=timezone(timedelta(hours=9)))
    ),
    "next_available": _info(next_available=date(2026, 11, 9)),
    "note": _info(note="Gebyret afhænger af tiden (19-51 kr.)."),
    "chosen_and_next": _info(
        chosen_start=datetime(2026, 11, 3, 16), next_available=date(2030, 1, 1)
    ),
}
QUOTE_LISTS = {
    "week": WEEK_LIST,
    "rugbroed": ["rugbrød"],
    "missing": ["findes ikke xyz"],
    "milk_and_missing": ["2 x letmælk 1 l", "findes ikke xyz", "toiletpapir"],
    "sizes": ["kaffe 400 g", "hakket oksekød 1,5 kg", "6 x bananer", "pasta"],
}
SHORT = ("none", "chosen_utc_after_midnight_in_dk", "next_available", "note")


class Scenario(CatalogStore):
    """A catalog store whose delivery info and terms the scenario sets."""

    def __init__(self, catalog, terms, info=None, fails=None) -> None:
        super().__init__()
        self.catalog, self.terms_, self._info, self._fails = catalog, terms, info or _info(), fails

    async def delivery_info(self):
        if self._fails == "not_implemented":
            raise NotImplementedError
        if self._fails == "store_error":
            raise registry.StoreError("ingen leveringsinfo")
        return self._info


class Broken(CatalogStore):
    async def search(self, query, limit=10):
        raise registry.StoreError("API'et svarer 500")


class Search:
    """A store for the sort modes: it records how it was called."""

    name = "s"

    def __init__(self, products) -> None:
        self.products, self.calls = products, []

    async def search(self, query, limit=10):
        self.calls.append([query, limit])
        return list(self.products)[:limit]


def best_match_file(name: str, entries: list, items: list[str]) -> dict:
    products = [p for _s, p in entries]
    index = {id(p): i for i, p in enumerate(products)}

    def run(item: str, subset: list[int], day: date | None) -> dict:
        line = compare.best_match(item, [products[i] for i in subset], day)
        out = server._out(line)
        out["product_index"] = None if line.product is None else index[id(line.product)]
        return out

    everything = list(range(len(products)))
    full = {item: {day_key(d): run(item, everything, d) for d in DAYS} for item in items}
    per_search: dict[str, dict] = {}
    for group, subset in groups(entries).items() if name != "synthetic" else []:
        relevant = [
            item
            for item in items
            if any(score(parse_item(item)[1], products[i], asked(item)) > 0 for i in subset)
        ]
        per_search[group] = {
            "indexes": subset,
            "results": {
                item: {day_key(d): run(item, subset, d) for d in DAYS} for item in relevant
            },
        }
    return {
        "corpus": name,
        "days": [day_key(d) for d in DAYS],
        "all": full,
        "per_search": per_search,
    }


def _case(name: str, items: list[str], quote, store) -> dict:
    return {"name": name, "items": items, "searches": store.searches, "quote": server._out(quote)}


async def quote_cases() -> list[dict]:
    out = []
    stores = {"nemlig": (Nemlig.catalog, TERMS_N), "bilkatogo": (Bilka.catalog, TERMS_B)}
    for store, (catalog, terms) in stores.items():
        for lname, items in QUOTE_LISTS.items():
            for dname, info in DELIVERY.items():
                if dname not in SHORT and lname != "sizes":
                    continue
                s = Scenario(catalog, terms, info)
                out.append(
                    _case(f"{store}/{lname}/{dname}", items, await compare.quote(s, items), s)
                )
    one = ["rugbrød"]
    extra = {
        "delivery_info_not_implemented": Scenario(Nemlig.catalog, TERMS_N, None, "not_implemented"),
        "delivery_info_store_error": Scenario(Nemlig.catalog, TERMS_N, None, "store_error"),
        "pickup_with_note": Scenario(
            Bilka.catalog, PICKUP, _info(note="Kurven er sat til afhentning.")
        ),
        "pickup_no_note": Scenario(Bilka.catalog, PICKUP),
    }
    for label, minimum in (
        ("exactly_met", Decimal("18.00")),
        ("none", None),
        ("fraction", Decimal("100.005")),
        ("huge", Decimal("1E+3")),
        ("zero", Decimal(0)),
    ):
        terms = StoreTerms(
            "bilkatogo", True, Decimal("39.5"), {"x": Decimal(0), "y": Decimal("0.1")}, minimum
        )
        extra[f"terms_minimum_{label}"] = Scenario(Bilka.catalog, terms)
    for name, store in extra.items():
        out.append(_case(name, one, await compare.quote(store, one), store))
    return out


async def verdict_cases() -> list[dict]:
    """compare_basket end to end: verdict and stores together, with the registry patched."""

    def make(catalog, terms):
        return lambda: Scenario(catalog, terms)

    nemlig, bilka = make(Nemlig.catalog, TERMS_N), make(Bilka.catalog, TERMS_B)
    setups = {
        "both": {"nemlig": nemlig, "bilkatogo": bilka},
        "one_ok_one_broken": {"nemlig": nemlig, "broken": Broken},
        "two_ok_one_broken": {"nemlig": nemlig, "bilkatogo": bilka, "broken": Broken},
        "all_broken": {"a": Broken, "b": Broken},
        "single": {"nemlig": nemlig},
        "pickup_and_home": {"nemlig": nemlig, "bilkatogo": make(Bilka.catalog, PICKUP)},
    }
    lists = {
        "week": WEEK_LIST,
        "rugbroed": ["rugbrød"],
        "none_common": ["findes ikke xyz"],
        "toilet_and_pasta": ["toiletpapir", "pasta 500 g"],
        "padded": [" mælk 1 l ", "", "2 x rugbrød"],
        "empty": [" ", ""],
        "too_long": ["mælk"] * (server.MAX_ITEMS + 1),
    }
    original = registry.STORES
    out = []
    try:
        for sname, stores in setups.items():
            for lname, items in lists.items():
                registry.STORES, server._instances = dict(stores), {}
                try:
                    answer = {"answer": await server.compare_basket(items)}
                except Exception as exc:  # noqa: BLE001  # a ToolError is part of the contract
                    answer = {"tool_error": f"{type(exc).__name__}: {exc}"}
                out.append({"name": f"{sname}/{lname}", "items": items, **answer})
    finally:
        registry.STORES, server._instances = original, {}
    return out


def _prod(pid: str, price: str, unit_price: str | None) -> Product:
    unit = None if unit_price is None else Decimal(unit_price)
    return Product("x", pid, pid, Decimal(price), unit_price=unit)


async def search_cases() -> list[dict]:
    sets = {
        "mixed": [_prod("a", "30", None), _prod("b", "20", "80"), _prod("c", "10", "90")],
        "ties": [
            _prod("t1", "10", "5"),
            _prod("t2", "10", "5"),
            _prod("t3", "9.99", "5.00"),
            _prod("t4", "10.00", "4"),
        ],
        "gift_first": [_prod("bread", "25", "25"), _prod("gift", "0", "0")],
        "all_unknown": [_prod("u1", "5", None), _prod("u2", "4", None)],
        "decimals": [
            _prod("d1", "0.125", "0.0001"),
            _prod("d2", "0.12", "0.00010"),
            _prod("d3", "1e1", "1E+1"),
        ],
        "empty": [],
    }
    original = registry.STORES
    out = []
    try:
        for sname, products in sets.items():
            runs = [
                (sort, limit)
                for sort in ("relevance", "price", "unit_price")
                for limit in (1, 2, 3, 10)
            ]
            if sname == "mixed":
                runs += [("price", limit) for limit in (-5, 0, 25, 26, 1000)]
            for sort, limit in runs:
                store = Search(products)
                registry.STORES, server._instances = {"s": lambda store=store: store}, {}
                answer = await server.search_products("kylling", ["s"], limit, sort)
                out.append(
                    {
                        "set": sname,
                        "sort": sort,
                        "limit": limit,
                        "calls": store.calls,
                        "ids": [p["id"] for p in answer["s"]],
                        "answer": answer,
                    }
                )
    finally:
        registry.STORES, server._instances = original, {}
    return out


def export(loop) -> None:
    items = all_items()
    for name, entries in corpora().items():
        rel = f"compare/best_match_{name}.json"
        dump(rel, best_match_file(name, entries, items))
    for rel, make in (
        ("quote", quote_cases),
        ("verdict", verdict_cases),
        ("search_sort", search_cases),
    ):
        dump(f"compare/{rel}.json", {"cases": loop.run_until_complete(make())})
