"""match goldens: the corpora, how items and sizes parse, and one row per (item, product) pair.

Pairs are sparse: only rows with score above 0 are written. Go must assert score 0 for every
pair that is not in a file (tests/golden/README.md).
"""

from __future__ import annotations

from datetime import date

from handle_mcp.match import (
    asked,
    brand_only,
    category_rank,
    flavour_only,
    is_fresh,
    is_oeko,
    mixed,
    pack_size,
    parse_item,
    parse_size,
    prepared,
    processed,
    processed_rank,
    query,
    score,
)
from handle_mcp.pricing import cost, packs, per_unit, pieces, too_much
from scripts.golden_corpora import corpora, product_json
from scripts.golden_io import dump, plain, q10, ser
from scripts.golden_synth import items as all_items

DAYS = [None, date(2026, 11, 2), date(2030, 1, 1)]  # 2030 expires every offer


def day_key(day: date | None) -> str:
    return "null" if day is None else day.isoformat()


def parse_one(item: str) -> dict:
    quantity, words, size = parse_item(item)
    return {
        "asked": sorted(asked(item)),
        "query": query(item),
        "quantity": quantity,
        "size": ser(size),
        "words": words,
    }


def product_facts(p) -> dict:
    """Facts that do not depend on the item."""
    return {
        "is_fresh": is_fresh(p),
        "is_oeko": is_oeko(p),
        "pack_size": ser(pack_size(p)),
        "parse_size_name": ser(parse_size(p.name)),
        "parse_size_size": ser(parse_size(p.size)),
        "processed": processed(p),
    }


def pair(item: str, i: int, p) -> dict | None:
    quantity, words, size = parse_item(item)
    sc = score(words, p, asked(item))
    if sc <= 0:
        return None
    n_packs, n_pieces = packs(size, quantity, p), pieces(size, quantity, p)
    count = n_packs or n_pieces or quantity
    by_day = {}
    for day in DAYS:
        c = cost(p, count, day)
        kind, each = per_unit(p, quantity, count, day)
        by_day[day_key(day)] = {"cost": plain(c), "per_unit": [kind, q10(each)]}
    return {
        "p": i,
        "score": sc,
        "prepared": prepared(words, p),
        "processed_rank": processed_rank(words, p),
        "processed_rank_fresh": processed_rank(words, p, True),
        "mixed": mixed(words, p),
        "flavour_only": flavour_only(words, p),
        "brand_only": brand_only(words, p),
        "category_rank": category_rank(words, p),
        "packs": n_packs,
        "pieces": n_pieces,
        "count": count,
        "too_much": too_much(size, quantity, count, p) if size else None,
        "by_day": by_day,
    }


def export() -> dict:
    entries = corpora()
    items = all_items()
    sizes, stats = {}, {"items": len(items), "corpora": {}, "pairs": {}}
    shared = {}
    for name, rows in entries.items():
        products = [p for _s, p in rows]
        body = {
            "corpus": name,
            "products": [{"source": s, "product": product_json(p)} for s, p in rows],
        }
        sizes[f"match/corpus_{name}.json"] = dump(f"match/corpus_{name}.json", body)
        stats["corpora"][name] = len(rows)
        pairs: dict[str, list] = {}
        for item in items:
            found = [r for i, p in enumerate(products) if (r := pair(item, i, p))]
            if found:
                pairs[item] = found
        out = {"days": [day_key(d) for d in DAYS], "corpus": name, "items": items, "pairs": pairs}
        sizes[f"match/pairs_{name}.json"] = dump(f"match/pairs_{name}.json", out)
        stats["pairs"][name] = sum(len(v) for v in pairs.values())
        shared[name] = [product_facts(p) for p in products]
    texts = {t for rows in entries.values() for _s, p in rows for t in (p.name, p.size) if t}
    parse = {
        "items": {item: parse_one(item) for item in items},
        "parse_size": {t: ser(parse_size(t)) for t in sorted(texts | set(items))},
        "products": shared,
    }
    sizes["match/parse.json"] = dump("match/parse.json", parse)
    stats["sizes"] = sizes
    return stats
