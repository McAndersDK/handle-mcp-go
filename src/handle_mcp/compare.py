"""compare_basket: price one shopping list in each store.

Simple and explainable matching. No store-specific code here.
- "2 x rugbrød" means quantity 2. A size like "1,5 kg" is the amount wanted: a 500 g pack
  is bought 3 times. Products are compared on the cost of the amount wanted.
- A product matches well when every word of the item is a whole word in its name.
- A word only after "m.", "med", "i", "u." ... is a flavour, not the product: "Yoghurt m. banan"
  is not a banana. Such products never win.
- On the same name score, a product in a matching store category wins ("Fisk > Laks").
- Then a product with a known size wins, then the lowest cost. Sold-out products never win.
- Unsure matches and missing items are marked, so a human can check them.
"""

from __future__ import annotations

import math
import re
from dataclasses import dataclass, field
from datetime import date, datetime
from decimal import Decimal
from itertools import pairwise
from typing import Any
from zoneinfo import ZoneInfo

from handle_mcp.models import Product
from handle_mcp.stores import StoreError

SEARCH_LIMIT = 10
DK = ZoneInfo("Europe/Copenhagen")  # offers end on Danish days
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
MULTI_RE = re.compile(r"(\d+)\s*for\s*(\d+(?:[.,]\d+)?)", re.IGNORECASE)  # "4 for 50 kr"
# Words that only say how a thing is made or packed: "frilandsæg" is æg, "splitlinser" is linser.
DESCRIBERS = ("frilands", "skrabe", "øko", "split", "mini", "snack", "baby")
# Frozen or canned. Never the default for fresh food: "blåbær" is fresh, "frosne blåbær" is not.
CANNED = ("konserv", "dåse", "juice")  # "Cherrytomater i tomatjuice", "Kolonial > Konserves"
FROZEN = ("frost", "frosne", "frossen", "frosset")
FRESH = ("frugt", "grønt", "bær", "grøntsag")  # frozen loses only here: frozen meat is fine
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
    """1.0 when every word is a whole word in the name, or its Danish plural or singular
    ('bananer' ~ 'banan'). Half for a word inside another word. A quarter for a word only in the
    flavour part of the name ('Yoghurt m. banan')."""
    # c and k are the same sound in Danish shop names: "Clementiner" ~ "klementiner".
    words = [w.replace("c", "k") for w in words]
    name = (_get(product, "name") or "").replace("C", "K").replace("c", "k")
    brand = (_get(product, "brand") or "").replace("C", "K").replace("c", "k")
    head = f"{FLAVOUR_RE.split(name)[0]} {brand}".lower()
    head_words = _words(head)
    # A compound split in two: "Cherry tomater" is "cherrytomater".
    tokens = set(head_words) | {a + b for a, b in pairwise(head_words)}
    tail = name.lower()
    tail_tokens = set(_words(tail))
    hits = []
    for w in words:
        if w in tokens or any(_plural(w, t) or _described(w, t) for t in tokens):
            hits.append(1.0)
        elif any(_compound(w, t) for t in tokens):
            hits.append(0.75)  # 'frilandsæg' for 'æg': better than a word inside, below whole
        elif w in head or _stem_hit(w, tokens):
            hits.append(0.5)
        else:
            hits.append(0.25 if w in tail or _stem_hit(w, tail_tokens) else 0.0)
    return sum(hits) / len(hits) if hits else 0.0


def _plural(word: str, token: str) -> bool:
    """Danish singular and plural are the same product: 'pære' ~ 'pærer', 'banan' ~ 'bananer'."""
    short, long_ = sorted((word, token), key=len)
    return len(short) >= 3 and long_ in (short + "r", short + "er", short + "e")


def _described(word: str, token: str) -> bool:
    """'frilandsæg' is plain æg: the first part only says how it is made."""
    return any(token == d + word for d in DESCRIBERS)


def _processed_rank(words: list[str], product: Any) -> int:
    """1 for canned food, or frozen fruit and vegetables, when the item does not ask for it.
    Frozen meat or fish is not demoted: a frozen 35 kr offer is a fine 'kyllingelår'."""
    text = f"{_get(product, 'name')} {_get(product, 'category') or ''}".lower()
    item = " ".join(words)
    if any(m in text for m in CANNED) and not any(m in item for m in CANNED):
        return 1
    produce = any(m in text for m in FRESH) or any(w.endswith("bær") for w in words)
    frozen = any(m in text for m in FROZEN) and not any(m in item for m in FROZEN)
    return 1 if frozen and produce else 0


def _price(product: Any, on: date | None) -> Decimal:
    """The price on the delivery day: the offer is gone after its last day."""
    until, before = _get(product, "offer_until"), _get(product, "before_price")
    if on and until and before and on > until:
        return Decimal(before)
    return Decimal(_get(product, "price"))


def _cost(product: Any, count: int, on: date | None) -> Decimal:
    """Price for `count` packs, with a multi-buy offer like '4 for 50 kr' when it is still on."""
    price = _price(product, on)
    match = MULTI_RE.search(_get(product, "offer_text") or "")
    until = _get(product, "offer_until")
    if match and not (on and until and on > until):
        size, total = int(match.group(1)), Decimal(match.group(2).replace(",", "."))
        if size > 1 and total < price * size:
            return (count // size) * total + (count % size) * price
    return price * count


def _compound(word: str, token: str) -> bool:
    """Danish compound words name the thing last: 'frilandsæg' is æg, 'splitlinser' is linser.
    The token needs 3 more letters, so 'post' is not 'ost'."""
    return len(token) >= len(word) + 3 and token.endswith(word)


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


def _pack_size(product: Any) -> tuple[str, Decimal] | None:
    return parse_size(_get(product, "size")) or parse_size(_get(product, "name"))


def _packs(wanted: tuple[str, Decimal] | None, quantity: int, product: Any) -> int | None:
    """How many packs give the wanted amount. '1,5 kg' with 500 g packs → 3.
    None when the sizes cannot be compared (no size, or kg against stk)."""
    have = _pack_size(product)
    if not wanted or not have or wanted[0] != have[0] or not have[1]:
        return None
    need = wanted[1] * quantity * (1 - SIZE_TOLERANCE)  # 15 % short still counts
    return max(1, math.ceil(need / have[1]))


def _too_much(wanted: tuple[str, Decimal], quantity: int, count: int, product: Any) -> bool:
    """True when the packs give over 15 % more than wanted: 500 g for '400 g'."""
    have = _pack_size(product)
    return bool(have) and have[1] * count > wanted[1] * quantity * (1 + SIZE_TOLERANCE)


def best_match(item: str, products: list[Any], on: date | None = None) -> BasketLine:
    """Best word match first, then a comparable size, then the lowest cost for the amount.
    `on` is the delivery day: offers that end before it do not count."""
    quantity, words, size = parse_item(item)
    ranked = []
    for product in products:
        score = _score(words, product)
        if _get(product, "available") and score >= 0.5:
            packs = _packs(size, quantity, product)
            size_rank = 0 if packs or not size else 1
            count = packs or quantity
            cost = _cost(product, count, on)
            # Canned or frozen produce loses before the category counts: a canned product can sit
            # in a category that fits ("... > Cherrytomater") while the fresh one does not.
            fit = (_processed_rank(words, product), _category_rank(words, product))
            have = _pack_size(product)
            amount = have[1] * count if have else Decimal(0)
            ranked.append((-score, fit, size_rank, cost, -amount, count, product))
    if not ranked:
        # The store's own top hit, e.g. "Penne" for "pasta". Never sure, but better than nothing.
        # Never a flavour-only hit ("Grøn te m. mandarin" for "mandariner").
        top = next(
            (x for x in products if _get(x, "available") and not _flavour_only(words, x)), None
        )
        if top is None:
            return BasketLine(item, quantity, None, Decimal(0), False, "Ikke fundet")
        note = "Butikkens bedste søgeresultat. Navnet matcher ikke. Tjek varen."
        return BasketLine(item, quantity, top, _cost(top, quantity, on), False, note, guess=True)
    # Same cost: more food wins, then fewer packs. Never the store's search order.
    neg_score, _cat, size_rank, cost, _amount, count, product = min(ranked, key=lambda r: r[:6])
    too_much = size_rank == 0 and size is not None and _too_much(size, quantity, count, product)
    sure = neg_score == -1.0 and size_rank == 0 and not too_much
    note = None
    if too_much:
        note = f"Anden størrelse: {count} x {_get(product, 'size') or _get(product, 'name')}."
    elif size_rank == 1:
        note = (
            f"Størrelsen kan ikke sammenlignes: {_get(product, 'size') or _get(product, 'name')}."
        )
    elif not sure:
        note = "Usikkert match. Tjek varen."
    elif size and count != quantity:
        note = f"{count} x {_get(product, 'size') or 'pakke'} for at nå mængden."
    until = _get(product, "offer_until")
    if on and until and on > until and _get(product, "before_price"):
        note = " ".join(filter(None, [note, f"Tilbuddet slutter {until:%d/%m}, før levering."]))
    return BasketLine(item, count, product, cost, sure, note)


async def quote(store: Any, items: list[str]) -> BasketQuote:
    """Search each item in one store (one call at a time) and add fees and minimum order."""
    try:  # read-only; e.g. a fee that depends on the slot. Prices still count without it.
        info = await store.delivery_info()
        note, on = _get(info, "note"), _day(info)
    except (NotImplementedError, StoreError):
        note, on = None, None
    lines = [best_match(item, await store.search(_query(item), SEARCH_LIMIT), on) for item in items]
    terms = await store.terms()
    found = [ln for ln in lines if ln.product is not None]
    products_total = sum((ln.line_total for ln in found), Decimal(0))
    fees = {k: v for k, v in {"levering": terms.delivery_fee, **terms.other_fees}.items() if v}
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


def _day(info: Any) -> date | None:
    """The delivery day: the chosen slot, else the next free one."""
    start = _get(info, "chosen_start") if info is not None else None
    if isinstance(start, datetime):
        return (start.astimezone(DK) if start.tzinfo else start).date()
    nxt = _get(info, "next_available") if info is not None else None
    return nxt if isinstance(nxt, date) else None


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
