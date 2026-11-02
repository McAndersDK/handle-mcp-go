"""compare_basket: price one shopping list in each store.

Simple and explainable matching. No store-specific code here.
- "2 x rugbrød" means quantity 2. A size like "1,5 kg" is the amount wanted: a 500 g pack
  is bought 3 times. Products are compared on the cost of the amount wanted.
- A product matches well when every word of the item is a whole word in its name.
- A word only after "m.", "med", "i", "u." ... is a flavour, not the product: "Yoghurt m. banan"
  is not a banana. Such products never win.
- On the same name score, a product in a matching store category wins ("Fisk > Laks").
- Then a product with a known size wins, then the lowest cost. Sold-out products never win.
- No size in the item: compare price per kg, l or piece, and ask for an amount.
- A blend ("m. 50% grønt") loses to the plain product and is never sure.
- A multi-buy offer ("3 for 48 kr") counts for the packs we need, also in the price per kg.
- An øko product is always among the alternatives when the search found one.
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
SIZE_TOLERANCE = Decimal("0.15")  # up to 15 % MORE than wanted still counts as the same size
SHORT_TOLERANCE = Decimal("0.02")  # never less than wanted: 990 g is 1 kg, 900 g is not
ALTERNATIVES = 3
STOP_WORDS = {"og", "med", "i", "af", "en", "et", "til", "uden"}
UNITS = {
    "g": ("g", 1), "gr": ("g", 1), "kg": ("g", 1000),
    "ml": ("ml", 1), "cl": ("ml", 10), "dl": ("ml", 100),
    "l": ("ml", 1000), "ltr": ("ml", 1000), "liter": ("ml", 1000),
    "stk": ("stk", 1), "st": ("stk", 1),
    "ruller": ("stk", 1), "rulle": ("stk", 1), "rl": ("stk", 1),
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
BLEND_RE = re.compile(r"\d+(?:[.,]\d+)?\s*%\s*([a-zæøå]+)", re.IGNORECASE)  # "50% grønt"
NUTRIENTS = {"fedt", "protein", "sukker", "kakao", "alkohol", "salt", "frugt", "kulhydrat"}
QTY_RE = re.compile(r"^\s*(\d+)\s*[x×]\s+(?=\D)", re.IGNORECASE)
OEKO_RE = re.compile(r"\b(?:øko|økologiske?|organic)\b", re.IGNORECASE)  # not "Økonomi"


@dataclass
class BasketLine:
    item: str  # the line from the shopping list
    quantity: int
    product: Product | None
    line_total: Decimal
    sure: bool
    note: str | None = None
    guess: bool = False  # the store's top hit with no word match: left out of the verdict
    # Next best products, so the caller can swap a wrong pick without a new search.
    alternatives: list[dict[str, Any]] = field(default_factory=list)


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


def _multi(product: Any, on: date | None) -> tuple[int, Decimal] | None:
    """A multi-buy offer like '4 for 50 kr' that is still on: (4, 50)."""
    match = MULTI_RE.search(_get(product, "offer_text") or "")
    until = _get(product, "offer_until")
    if not match or (on and until and on > until):
        return None
    size, total = int(match.group(1)), Decimal(match.group(2).replace(",", "."))
    return (size, total) if size > 1 and total < _price(product, on) * size else None


def _cost(product: Any, count: int, on: date | None) -> Decimal:
    """Price for `count` packs, with a multi-buy offer like '4 for 50 kr' when it is still on."""
    price = _price(product, on)
    if offer := _multi(product, on):
        size, total = offer
        return (count // size) * total + (count % size) * price
    return price * count


def _multi_hint(product: Any, count: int, on: date | None) -> str | None:
    """Say it when the multi-buy is close: 2 packs at 22 kr, but '3 for 48 kr' is 4 kr more.
    We never buy more than needed on our own: the human decides."""
    offer = _multi(product, on)
    if not offer or count % offer[0] == 0:
        return None
    full = math.ceil(count / offer[0]) * offer[0]
    extra = _cost(product, full, on) - _cost(product, count, on)
    if extra >= _price(product, on):
        return None
    return f"Tilbud {_get(product, 'offer_text')}: {full - count} mere for {extra} kr."


def is_oeko(product: Any) -> bool:
    return bool(OEKO_RE.search(f"{_get(product, 'name')} {_get(product, 'brand') or ''}"))


def _compound(word: str, token: str) -> bool:
    """Danish compound words name the thing last: 'frilandsæg' is æg, 'splitlinser' is linser.
    The token needs 3 more letters, so 'post' is not 'ost'."""
    return len(token) >= len(word) + 3 and token.endswith(word)


def _mixed(words: list[str], product: Any) -> int:
    """1 for a blend the item does not ask for: 'Hakket oksekød m. 50% grønt'. A share of
    another food ('50% grønt') means a mix. A share of fat or sugar ('m. 0,5% fedt') does not,
    nor 'Rugbrød m. kerner' or 'Laks m. skind'."""
    parts = FLAVOUR_RE.split(_get(product, "name") or "", maxsplit=1)
    shares = BLEND_RE.findall(parts[1]) if len(parts) == 2 else []
    if not any(w.lower() not in NUTRIENTS for w in shares):
        return 0
    tail = set(_words(parts[1]))
    return 0 if any(w in tail or _stem_hit(w, tail) for w in words) else 1


def _per_unit(product: Any, quantity: int, count: int, on: date | None) -> tuple[int, Decimal]:
    """For an item with no size: compare price per kg, l or piece, not per pack.
    '4 x pærer' counts pieces, so pieces come first. 'æbler' is a bag, so weight comes first.
    The pack price is what `count` packs cost each, so '3 for 48 kr' counts for 3 packs."""
    have = _pack_size(product)
    price = _cost(product, count, on) / count
    if not have or not have[1]:
        return 2, price
    by_piece = have[0] == "stk"
    kind = (0 if by_piece else 1) if quantity > 1 else (1 if by_piece else 0)
    return kind, price / have[1]


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
    need = wanted[1] * quantity * (1 - SHORT_TOLERANCE)
    return max(1, math.ceil(need / have[1]))


def _pieces(wanted: tuple[str, Decimal] | None, quantity: int, product: Any) -> int | None:
    """'6 x bananer' with no size means 6 bananas: a 4-pack is bought twice, not 6 times."""
    have = _pack_size(product)
    if wanted or quantity < 2 or not have or have[0] != "stk" or have[1] <= 1:
        return None
    return math.ceil(quantity / have[1])


def _too_much(wanted: tuple[str, Decimal], quantity: int, count: int, product: Any) -> bool:
    """True when the packs give over 15 % more than wanted: 500 g for '400 g'."""
    have = _pack_size(product)
    return bool(have) and have[1] * count > wanted[1] * quantity * (1 + SIZE_TOLERANCE)


def best_match(item: str, products: list[Any], on: date | None = None) -> BasketLine:
    """The best product for one list item, plus the next best as alternatives."""
    line = _best(item, products, on)
    chosen = _get(line.product, "id") if line.product is not None else None
    others = []
    for x in products:
        if not _get(x, "available") or _get(x, "id") == chosen:
            continue
        alt = _best(item, [x], on)
        if alt.product is not None and not alt.guess:
            others.append((-_score(parse_item(item)[1], x), alt.line_total, alt))
    others.sort(key=lambda o: o[:2])
    picked = [a for _s, _t, a in others[:ALTERNATIVES]]
    # Always show an øko choice, so a human can pick it when it costs only a little more.
    if line.product is not None and not is_oeko(line.product):
        oeko = [a for _s, _t, a in others if is_oeko(a.product)]
        if oeko and oeko[0] not in picked:
            picked = [*picked[: ALTERNATIVES - 1], oeko[0]]
    line.alternatives = [_brief(a) for a in picked]
    return line


def _brief(line: BasketLine) -> dict[str, Any]:
    p = line.product
    return {
        "id": _get(p, "id"),
        "name": _get(p, "name"),
        "size": _get(p, "size"),
        "quantity": line.quantity,
        "line_total": line.line_total,
        "offer_text": _get(p, "offer_text"),
        "oeko": is_oeko(p),
        "note": line.note,
    }


def _best(item: str, products: list[Any], on: date | None = None) -> BasketLine:
    """Best word match first, then a comparable size, then the lowest cost for the amount.
    `on` is the delivery day: offers that end before it do not count."""
    quantity, words, size = parse_item(item)
    ranked = []
    for product in products:
        score = _score(words, product)
        if _get(product, "available") and score >= 0.5:
            packs = _packs(size, quantity, product) or _pieces(size, quantity, product)
            size_rank = 0 if packs or not size else 1
            count = packs or quantity
            cost = _cost(product, count, on)
            # Canned or frozen produce loses before the category counts: a canned product can sit
            # in a category that fits ("... > Cherrytomater") while the fresh one does not.
            fit = (
                _processed_rank(words, product),
                _category_rank(words, product),
                _mixed(words, product),
            )
            have = _pack_size(product)
            amount = have[1] * count if have else Decimal(0)
            value = (cost,) if size else _per_unit(product, quantity, count, on)
            ranked.append((-score, fit, size_rank, value, cost, -amount, count, product))
    if not ranked:
        return _guess(item, quantity, size, products, words, on)
    # Same cost: more food wins, then fewer packs. Never the store's search order.
    neg_score, fit, size_rank, _value, cost, _amount, count, product = min(
        ranked, key=lambda r: r[:7]
    )
    too_much = size_rank == 0 and size is not None and _too_much(size, quantity, count, product)
    mixed = fit[2] == 1
    sure = neg_score == -1.0 and size_rank == 0 and not too_much and not mixed
    note = None
    if too_much:
        note = f"Anden størrelse: {count} x {_get(product, 'size') or _get(product, 'name')}."
    elif size_rank == 1:
        note = (
            f"Størrelsen kan ikke sammenlignes: {_get(product, 'size') or _get(product, 'name')}."
        )
    elif mixed:
        note = f"Blandingsvare: {_get(product, 'name')}. Tjek varen."
    elif not sure:
        note = "Usikkert match. Tjek varen."
    elif size and count != quantity:
        note = f"{count} x {_get(product, 'size') or 'pakke'} for at nå mængden."
    if not size:
        hint = "Ingen mængde på listen: billigst pr. kg, l eller stk. Skriv en mængde, fx '1 kg'."
        note = " ".join(filter(None, [note, hint]))
    note = " ".join(filter(None, [note, _multi_hint(product, count, on)]))
    until = _get(product, "offer_until")
    if on and until and on > until and _get(product, "before_price"):
        note = " ".join(filter(None, [note, f"Tilbuddet slutter {until:%d/%m}, før levering."]))
    return BasketLine(item, count, product, cost, sure, note)


def _guess(
    item: str,
    quantity: int,
    size: tuple[str, Decimal] | None,
    products: list[Any],
    words: list[str],
    on: date | None,
) -> BasketLine:
    """No name matches, e.g. "Penne" for "pasta". Never sure and left out of the verdict.
    With a size: only products of that size kind, cheapest for the amount ("Tortelloni 250 g"
    is not a guess for "pasta 500 g" when "Fettuccine 500 g" is cheaper). Without: the store's
    top hit. Never a flavour-only hit ("Grøn te m. mandarin" for "mandariner")."""
    ok = [x for x in products if _get(x, "available") and not _flavour_only(words, x)]
    if size:
        sized = [(x, _packs(size, quantity, x)) for x in ok]
        sized = [(x, n) for x, n in sized if n]
        if not sized:
            return BasketLine(item, quantity, None, Decimal(0), False, "Ikke fundet")
        top, count = min(sized, key=lambda xn: _cost(xn[0], xn[1], on))
    elif ok:
        top, count = ok[0], quantity
    else:
        return BasketLine(item, quantity, None, Decimal(0), False, "Ikke fundet")
    note = "Butikkens bedste søgeresultat. Navnet matcher ikke. Tjek varen."
    return BasketLine(item, count, top, _cost(top, count, on), False, note, guess=True)


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
    if minimum is not None and 0 < products_total < minimum:  # in words, for the human
        notes.append(f"Under minimumskøb: {minimum - products_total} kr. mangler til {minimum} kr.")
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
