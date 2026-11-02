"""compare_basket: price one shopping list in each store.

Simple and explainable. No store-specific code here. Words are matched in `match`, kroner are
counted in `pricing`. This module picks the best product per item and compares the stores.
- Best word match first, then a comparable size, then the lowest cost for the amount.
- "2 x broccoli" with no size: the lowest cost for 2 packs wins, also 500 g against 1 stk.
  Only for pieces and produce: "2 x mælk" still compares per litre.
- A product that matches only by its brand is never a guess: "Fior di Frutta" jam.
- On the same name score, a product in a matching store category wins ("Fisk > Laks").
- Then a product with a known size wins, then the lowest cost. Sold-out products never win.
- An øko product is always among the alternatives when the search found one.
- Unsure matches and missing items are marked, so a human can check them.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import date, datetime
from decimal import Decimal
from typing import Any
from zoneinfo import ZoneInfo

from handle_mcp.match import (
    asked,
    brand_only,
    category_rank,
    flavour_only,
    get,
    is_fresh,
    is_oeko,
    mixed,
    pack_size,
    parse_item,
    processed_rank,
    query,
    score,
)
from handle_mcp.models import Product
from handle_mcp.pricing import cost, multi_hint, packs, per_unit, pieces, too_much
from handle_mcp.stores import StoreError

SEARCH_LIMIT = 30  # the cheapest tin was hit 14 for "hakkede tomater"

DK = ZoneInfo("Europe/Copenhagen")  # offers end on Danish days

ALTERNATIVES = 3


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
    oeko: bool = False  # the chosen product is øko


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


def best_match(item: str, products: list[Any], on: date | None = None) -> BasketLine:
    """The best product for one list item, plus the next best as alternatives."""
    line = _best(item, products, on)
    chosen = get(line.product, "id") if line.product is not None else None
    others = []
    for x in products:
        if not get(x, "available") or get(x, "id") == chosen:
            continue
        alt = _best(item, [x], on)
        if alt.product is not None and not alt.guess:
            others.append((-score(parse_item(item)[1], x, asked(item)), alt.line_total, alt))
    others.sort(key=lambda o: o[:2])
    picked = [a for _s, _t, a in others[:ALTERNATIVES]]
    # Always show an øko choice, so a human can pick it when it costs only a little more.
    if line.product is not None and not is_oeko(line.product):
        oeko = [a for _s, _t, a in others if is_oeko(a.product)]
        if oeko and oeko[0] not in picked:
            picked = [*picked[: ALTERNATIVES - 1], oeko[0]]
    line.alternatives = [_brief(a) for a in picked]
    line.oeko = line.product is not None and is_oeko(line.product)
    return line


def _brief(line: BasketLine) -> dict[str, Any]:
    p = line.product
    return {
        "id": get(p, "id"),
        "name": get(p, "name"),
        "size": get(p, "size"),
        "price": get(p, "price"),
        "quantity": line.quantity,
        "line_total": line.line_total,
        "offer_text": get(p, "offer_text"),
        "oeko": is_oeko(p),
        "note": line.note,
    }


def _best(item: str, products: list[Any], on: date | None = None) -> BasketLine:
    """Best word match first, then a comparable size, then the lowest cost for the amount.
    `on` is the delivery day: offers that end before it do not count."""
    quantity, words, size = parse_item(item)
    # The item is produce when the search found it fresh: then canned or frozen loses.
    produce = any(get(x, "available") and is_fresh(x) and score(words, x) >= 0.5 for x in products)
    ranked = []
    for product in products:
        name_score = score(words, product, asked(item))
        if get(product, "available") and name_score >= 0.5:
            n_packs = packs(size, quantity, product) or pieces(size, quantity, product)
            size_rank = 0 if n_packs or not size else 1
            count = n_packs or quantity
            line_cost = cost(product, count, on)
            # Canned or frozen produce loses before the category counts: a canned product can sit
            # in a category that fits ("... > Cherrytomater") while the fresh one does not.
            fit = (
                processed_rank(words, product, produce),
                category_rank(words, product),
                mixed(words, product),
            )
            have = pack_size(product)
            amount = have[1] * count if have else Decimal(0)
            # '6 x bananer' or '2 x broccoli': N things. Compare what they cost, also a 500 g
            # broccoli against '1 stk'. Not per piece or kg. '2 x mælk' still compares per litre.
            by_piece = not size and quantity > 1 and bool(have) and (have[0] == "stk" or produce)
            value = (
                (line_cost,)
                if size
                else (0, line_cost)
                if by_piece
                else per_unit(product, quantity, count, on)
            )
            ranked.append((-name_score, fit, size_rank, value, line_cost, -amount, count, product))
    if not ranked:
        return _guess(item, quantity, size, products, words, on)
    # Same cost: more food wins, then fewer packs. Never the store's search order.
    neg_score, fit, size_rank, _value, line_cost, _amount, count, product = min(
        ranked, key=lambda r: r[:7]
    )
    over = size_rank == 0 and size is not None and too_much(size, quantity, count, product)
    is_mixed = fit[2] == 1
    sure = neg_score == -1.0 and size_rank == 0 and not over and not is_mixed
    note = None
    if over:
        note = f"Anden størrelse: {count} x {get(product, 'size') or get(product, 'name')}."
    elif size_rank == 1:
        note = f"Størrelsen kan ikke sammenlignes: {get(product, 'size') or get(product, 'name')}."
    elif is_mixed:
        note = f"Blandingsvare: {get(product, 'name')}. Tjek varen."
    elif not sure:
        note = "Usikkert match. Tjek varen."
    elif size and count != quantity:
        note = f"{count} x {get(product, 'size') or 'pakke'} for at nå mængden."
    have = pack_size(product)
    if not size and quantity > 1 and have and have[0] == "stk":
        if have[1] * count > quantity:
            extra = f"{count} x {get(product, 'size') or 'pakke'} = {have[1] * count:g} stk."
            note = " ".join(filter(None, [note, extra]))
    elif not size:
        what = f"{quantity} pakker" if quantity > 1 and produce else "pr. kg, l eller stk"
        hint = f"Ingen mængde på listen: billigst {what}. Skriv en mængde, fx '1 kg'."
        note = " ".join(filter(None, [note, hint]))
    note = " ".join(filter(None, [note, multi_hint(product, count, on)]))
    until = get(product, "offer_until")
    if on and until and on > until and get(product, "before_price"):
        note = " ".join(filter(None, [note, f"Tilbuddet slutter {until:%d/%m}, før levering."]))
    return BasketLine(item, count, product, line_cost, sure, note)


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
    ok = [
        x
        for x in products
        if get(x, "available") and not flavour_only(words, x) and not brand_only(words, x)
    ]
    if size:
        sized = [(x, packs(size, quantity, x)) for x in ok]
        sized = [(x, n) for x, n in sized if n]
        if not sized:
            return BasketLine(item, quantity, None, Decimal(0), False, "Ikke fundet")
        top, count = min(sized, key=lambda xn: cost(xn[0], xn[1], on))
    elif ok:
        top, count = ok[0], quantity
    else:
        return BasketLine(item, quantity, None, Decimal(0), False, "Ikke fundet")
    note = "Butikkens bedste søgeresultat. Navnet matcher ikke. Tjek varen."
    return BasketLine(item, count, top, cost(top, count, on), False, note, guess=True)


async def quote(store: Any, items: list[str]) -> BasketQuote:
    """Search each item in one store (one call at a time) and add fees and minimum order."""
    try:  # read-only; e.g. a fee that depends on the slot. Prices still count without it.
        info = await store.delivery_info()
        note, on = get(info, "note"), _day(info)
    except (NotImplementedError, StoreError):
        note, on = None, None
    lines = [best_match(item, await store.search(query(item), SEARCH_LIMIT), on) for item in items]
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
    start = get(info, "chosen_start") if info is not None else None
    if isinstance(start, datetime):
        return (start.astimezone(DK) if start.tzinfo else start).date()
    nxt = get(info, "next_available") if info is not None else None
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
