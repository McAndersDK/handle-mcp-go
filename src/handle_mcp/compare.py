"""compare_basket: price one shopping list in each store.

Simple and explainable matching. No store-specific code here.
- "2 x rugbrød" means quantity 2. A size like "1 l" or "500 g" in the item must match.
- A product matches well when every word of the item is a whole word in its name.
- A word only after "m.", "med", "i", "u." ... is a flavour, not the product: "Yoghurt m. banan"
  is not a banana. Such products never win.
- On the same name score, a product in a matching store category wins ("Fisk > Laks").
- Then the right size wins, then the lowest price. Sold-out products never win.
- Unsure matches and missing items are marked, so a human can check them.
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from decimal import Decimal
from typing import Any

from handle_mcp.models import Product
from handle_mcp.stores import StoreError

SEARCH_LIMIT = 10
SIZE_TOLERANCE = Decimal("0.15")  # 15 % off the wanted size still counts as the same size
STOP_WORDS = {"og", "med", "i", "af", "en", "et", "til", "uden"}
UNITS = {
    "g": ("g", 1), "gr": ("g", 1), "kg": ("g", 1000),
    "ml": ("ml", 1), "cl": ("ml", 10), "dl": ("ml", 100),
    "l": ("ml", 1000), "ltr": ("ml", 1000), "liter": ("ml", 1000),
    "stk": ("stk", 1), "st": ("stk", 1),
}  # fmt: skip
SIZE_RE = re.compile(
    r"(?:(\d+)\s*[x×]\s*)?(\d+(?:[.,]\d+)?)\s*(" + "|".join(UNITS) + r")\b", re.IGNORECASE
)
FLAVOUR_RE = re.compile(r"\s(?:m\.|med|i|u\.|uden|smag)\s", re.IGNORECASE)
QTY_RE = re.compile(r"^\s*(\d+)\s*[x×]\s+(?=\D)", re.IGNORECASE)


@dataclass
class BasketLine:
    item: str  # the line from the shopping list
    quantity: int
    product: Product | None
    line_total: Decimal
    sure: bool
    note: str | None = None
    guess: bool = False  # the store's top hit with no word match: left out of the verdict


@dataclass
class BasketQuote:
    store: str
    lines: list[BasketLine]
    missing: list[str]
    products_total: Decimal
    fees: dict[str, Decimal]
    total: Decimal
    minimum_order: Decimal | None
    minimum_met: bool | None
    notes: list[str] = field(default_factory=list)


def _get(product: Any, key: str) -> Any:
    """Adapters return Product; tolerate a dict too."""
    return product.get(key) if isinstance(product, dict) else getattr(product, key)


def parse_size(text: str | None) -> tuple[str, Decimal] | None:
    """'1 l' → ('ml', 1000). '12 x 0,25 l' → ('ml', 3000). None if no size."""
    match = SIZE_RE.search(text or "")
    if not match:
        return None
    count, amount, unit = match.groups()
    base, factor = UNITS[unit.lower()]
    return base, Decimal(amount.replace(",", ".")) * factor * int(count or 1)


def parse_item(item: str) -> tuple[int, list[str], tuple[str, Decimal] | None]:
    """'2 x letmælk 1 l' → (2, ['letmælk'], ('ml', 1000))."""
    quantity = 1
    if match := QTY_RE.match(item):
        quantity, item = int(match.group(1)), item[match.end() :]
    size = parse_size(item)
    words = [w for w in _words(SIZE_RE.sub(" ", item)) if w not in STOP_WORDS]
    return max(quantity, 1), words, size


def _query(item: str) -> str:
    """What we send to the store's search: the words only. '2 x letmælk 1 l' → 'letmælk'."""
    return " ".join(parse_item(item)[1]) or item


def _words(text: str) -> list[str]:
    return re.findall(r"[a-zæøå0-9]+", text.lower())


def _score(words: list[str], product: Any) -> float:
    """1.0 when every word is a whole word in the name. Half for a word inside another word,
    or a Danish plural or singular ('bananer' ~ 'banan'). A quarter for a word only in the
    flavour part of the name ('Yoghurt m. banan')."""
    name = _get(product, "name") or ""
    head = f"{FLAVOUR_RE.split(name)[0]} {_get(product, 'brand') or ''}".lower()
    tokens, tail = set(_words(head)), name.lower()
    tail_tokens = set(_words(tail))
    hits = []
    for w in words:
        if w in tokens:
            hits.append(1.0)
        elif w in head or _stem_hit(w, tokens):
            hits.append(0.5)
        else:
            hits.append(0.25 if w in tail or _stem_hit(w, tail_tokens) else 0.0)
    return sum(hits) / len(hits) if hits else 0.0


def _flavour_only(words: list[str], product: Any) -> bool:
    """True when the item's words are only in the flavour part: 'Grøn te m. mandarin'."""
    return _score(words, product) > 0 and all(_score([w], product) < 0.5 for w in words)


def _stem_hit(word: str, tokens: set[str]) -> bool:
    return any(len(t) >= 4 and (word.startswith(t) or t.startswith(word)) for t in tokens)


def _category_rank(words: list[str], product: Any) -> int:
    """0 when an item word fits the store's category ('cherrytomater' ~ 'Tomater'), else 1."""
    tokens = set(_words(_get(product, "category") or ""))
    fits = _stem_hit if tokens else (lambda _w, _t: False)
    return 0 if any(fits(w, tokens) or any(_suffix(w, t) for t in tokens) for w in words) else 1


def _suffix(word: str, token: str) -> bool:
    """Danish compound words: 'cherrytomater' ends with 'tomater'. Short tokens like 'ost' are too
    loose ('post'), so both sides need 4+ letters."""
    return len(token) >= 4 and len(word) >= 4 and (word.endswith(token) or token.endswith(word))


def _size_ok(wanted: tuple[str, Decimal] | None, product: Any) -> bool | None:
    """True/False when both sizes are known, None when we cannot tell."""
    have = parse_size(_get(product, "size")) or parse_size(_get(product, "name"))
    if not wanted or not have or wanted[0] != have[0]:
        return None if not wanted or not have else False
    return abs(have[1] - wanted[1]) <= wanted[1] * SIZE_TOLERANCE


def best_match(item: str, products: list[Any]) -> BasketLine:
    """Best word match first, then the right size, then the lowest price."""
    quantity, words, size = parse_item(item)
    ranked = []
    for product in products:
        score = _score(words, product)
        if _get(product, "available") and score >= 0.5:
            size_ok = _size_ok(size, product)
            size_rank = 0 if size_ok or not size else 1 if size_ok is None else 2
            price = Decimal(_get(product, "price"))
            ranked.append((-score, _category_rank(words, product), size_rank, price, product))
    if not ranked:
        # The store's own top hit, e.g. "Penne" for "pasta". Never sure, but better than nothing.
        # Never a flavour-only hit ("Grøn te m. mandarin" for "mandariner").
        top = next(
            (x for x in products if _get(x, "available") and not _flavour_only(words, x)), None
        )
        if top is None:
            return BasketLine(item, quantity, None, Decimal(0), False, "Ikke fundet")
        price = Decimal(_get(top, "price"))
        note = "Butikkens bedste søgeresultat. Navnet matcher ikke. Tjek varen."
        return BasketLine(item, quantity, top, price * quantity, False, note, guess=True)
    neg_score, _cat, size_rank, price, product = min(ranked, key=lambda r: r[:4])
    sure = neg_score == -1.0 and size_rank == 0
    note = None
    if size_rank == 2:
        note = f"Anden størrelse: {_get(product, 'size') or _get(product, 'name')}."
    elif not sure:
        note = "Usikkert match. Tjek varen."
    return BasketLine(item, quantity, product, price * quantity, sure, note)


async def quote(store: Any, items: list[str]) -> BasketQuote:
    """Search each item in one store (one call at a time) and add fees and minimum order."""
    lines = [best_match(item, await store.search(_query(item), SEARCH_LIMIT)) for item in items]
    terms = await store.terms()
    found = [ln for ln in lines if ln.product is not None]
    products_total = sum((ln.line_total for ln in found), Decimal(0))
    fees = {k: v for k, v in {"levering": terms.delivery_fee, **terms.other_fees}.items() if v}
    try:  # read-only; e.g. a fee that depends on the slot. Prices still count without it.
        note = _get(await store.delivery_info(), "note")
    except (NotImplementedError, StoreError):
        note = None
    notes = [note] if note else []
    if not terms.home_delivery and not note:
        notes.append("Kurven er ikke sat til hjemlevering.")
    minimum = terms.minimum_order
    return BasketQuote(
        store=terms.store,
        lines=found,
        missing=[ln.item for ln in lines if ln.product is None],
        products_total=products_total,
        fees=fees,
        total=products_total + sum(fees.values(), Decimal(0)),
        minimum_order=minimum,
        minimum_met=None if minimum is None else products_total >= minimum,
        notes=notes,
    )


def verdict(quotes: dict[str, Any]) -> dict[str, Any]:
    """Compare stores on the items every store has, plus each store's fees."""
    ok = {n: q for n, q in quotes.items() if isinstance(q, BasketQuote)}
    failed = ", ".join(n for n in quotes if n not in ok)
    if len(ok) < 2:
        priced = (
            f"Kun {', '.join(ok)} kunne prise listen." if ok else "Ingen butik kunne prise listen."
        )
        return {"cheapest": None, "summary": priced + (f" Fejl: {failed}." if failed else "")}
    common = set.intersection(*({ln.item for ln in q.lines if not ln.guess} for q in ok.values()))
    if not common:
        return {"cheapest": None, "summary": "Ingen vare blev fundet i alle butikker. Tjek listen."}
    totals = {
        n: sum((ln.line_total for ln in q.lines if ln.item in common), Decimal(0))
        + sum(q.fees.values(), Decimal(0))
        for n, q in ok.items()
    }
    cheapest = min(totals, key=lambda n: totals[n])
    summary = f"Billigst: {cheapest} ({totals[cheapest]} kr. inkl. gebyrer"
    summary += f" for de {len(common)} varer, alle butikker har)."
    if any(q.missing for q in ok.values()) or any(
        not ln.sure for q in ok.values() for ln in q.lines
    ):
        summary += " Nogle varer mangler eller er usikre. Tjek dem."
    if failed:
        summary += f" Fejl: {failed}."
    return {"cheapest": cheapest, "comparable_totals": totals, "summary": summary}
