"""The corpora the match and compare goldens run on.

nemlig and bilkatogo: products parsed by the real adapters from tests/fixtures. synthetic: the
products the tests build by hand. Every product keeps its source, so a run can also be done on
one search answer at a time (that is how a store answers).
"""

from __future__ import annotations

import json
from datetime import date
from decimal import Decimal

import httpx

from handle_mcp.models import Product
from handle_mcp.stores.bilkatogo import BilkaStore
from handle_mcp.stores.nemlig import NemligStore
from handle_mcp.stores.nemlig_offers import OFFER_LISTS, offer_product
from scripts.golden_io import ROOT, raw
from scripts.golden_synth import synthetic

FIX = ROOT / "tests" / "fixtures"
NEMLIG_SEARCH = [
    "search",
    "search_gift",
    "search_kyllingebryst",
    "match_broccoli",
    "match_hakkede_tomater",
    "match_hakkede_tomater_30",
    "match_hvidloeg",
    "match_makrel_tomat",
]
BILKA_SEARCH = [
    "algolia_products",
    "algolia_offers",
    "match_fior_di_latte",
    "match_hakkede_tomater",
    "match_hvidloeg",
    "match_makrel_tomat",
    "match_mozzarella",
]


def load(store: str, name: str) -> dict:
    return json.loads((FIX / store / f"{name}.json").read_text(encoding="utf-8"))


def nemlig() -> list[tuple[str, Product]]:
    adapter = NemligStore(httpx.AsyncClient())
    out = []
    for name in NEMLIG_SEARCH:
        for i, hit in enumerate(load("nemlig", name)["Products"]["Products"]):
            out.append((f"{name}.json#{i}", adapter._product(hit)))
    page = load("nemlig", "offers_page")
    for block in page["pageContent"]:
        head = str((block.get("header") or {}).get("title") or "").strip().lower()
        if head not in OFFER_LISTS:
            continue
        for i, item in enumerate(block.get("products") or []):
            if product := offer_product(item, OFFER_LISTS[head]):
                out.append((f"offers_page.json#{OFFER_LISTS[head]}#{i}", product))
    return out


def bilka() -> list[tuple[str, Product]]:
    adapter = BilkaStore(httpx.AsyncClient())
    out = []
    for name in BILKA_SEARCH:
        for i, hit in enumerate(load("bilkatogo", name)["hits"]):
            for sid in sorted(hit.get("storeData") or {}):
                if product := adapter._product(hit, sid):
                    out.append((f"{name}.json#{i}@{sid}", product))
    return out


def corpora() -> dict[str, list[tuple[str, Product]]]:
    return {
        "nemlig": nemlig(),
        "bilkatogo": bilka(),
        "synthetic": [(f"synthetic#{i}", x) for i, x in enumerate(synthetic())],
    }


def product_json(p: Product) -> dict:
    """A corpus input: Decimals keep their exact str(), dates are ISO."""
    out: dict = {}
    for key, value in vars(p).items():
        if isinstance(value, Decimal):
            out[key] = raw(value)
        elif isinstance(value, date):
            out[key] = value.isoformat()
        else:
            out[key] = value
    return out


def groups(entries: list[tuple[str, Product]]) -> dict[str, list[int]]:
    """Corpus indexes per source file (or per synthetic list): one search answer each."""
    out: dict[str, list[int]] = {}
    for i, (source, _p) in enumerate(entries):
        out.setdefault(source.split("#")[0].split("@")[0], []).append(i)
    return out
