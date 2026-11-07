"""Strip personal data before anything leaves the server.

Every tool answer goes through `scrub()`. Whatever a tool returns lands in the
model's context and the chat transcript, so name, address, phone, email, door
code and delivery notes must never be in it.

Values are replaced with a marker instead of deleted, so the model can still
tell that an order *had* a delivery address. Keys are matched without case,
`_` or `-`, so `doorCode`, `DoorCode` and `door_code` are the same key.

Free text from a store (warnings, messages, errors) can echo personal data
under a harmless key. `scrub_text()` replaces emails, phone numbers and
addresses in it. It runs on the free-text keys below and on every error text,
not on product names: "Kildevand 0,5 l 24 stk" must pass as the store wrote it.
False positives are accepted in free text: an 8-digit id reads as a phone number
and "1000 Stk" as a postal code and city. Better a hidden id than a leaked phone.
"""

from __future__ import annotations

import dataclasses
import re
from datetime import date, datetime
from decimal import ROUND_HALF_UP, Decimal, InvalidOperation
from typing import Any

REDACTED = "[fjernet af handle-mcp]"

PERSONAL_KEYS = frozenset(
    {
        # whole blocks that are nothing but personal data
        "invoiceaddress",
        "deliveryaddress",
        "billingaddress",
        "homeaddress",
        # names (not "name": that is the product name)
        "customername",
        "contactperson",
        "recipientname",
        "fullname",
        "firstname",
        "lastname",
        "username",
        "membername",
        # contact
        "email",
        "mail",
        "emailaddress",
        "phone",
        "phonenumber",
        "mobile",
        "mobilenumber",
        "mobilephone",
        "telephone",
        # where we live and how to get in
        "address",
        "street",
        "streetname",
        "housenumber",
        "floor",
        "door",
        "zip",
        "zipcode",
        "postalcode",
        "postcode",
        "city",
        "lat",
        "lng",
        "latitude",
        "longitude",
        "doorcode",
        "unattendednotes",
        "deliverycomment",
        "deliverynote",
        "deliverynotes",
        "deliveryplacement",
        "pickupcode",
        "orderpickupcode",
        "dawauid",  # Bilka: DAWA id that points to one Danish address
        # account ids
        "uid",
        "tid",
        "customerid",
        "memberid",
        "userid",
        # payment (never fetched, but never passed on either)
        "card",
        "cards",
        "cardnumber",
        "maskedcardnumber",
        "creditcard",
        "iban",
    }
)


# store free text: the value may hold personal data even though the key does not
FREE_TEXT_KEYS = frozenset({"warnings", "message", "messages", "note", "error"})

_STREET_END = (
    "vej|gade|allé|alle|vænge|vænget|stræde|plads|torv|boulevard|parken|park|"
    "haven|have|bakken|toften|engen|marken|stien|sti|høj|lund|ager"
)
_STREET_WORD = "Vej|Gade|Allé|Alle|Boulevard|Plads|Torv|Stræde|Vænge"
_TEXT_PATTERNS = (
    re.compile(r"[\w.+-]+@[\w-]+(?:\.[\w-]+)+"),  # email
    re.compile(r"(?<![0-9])(?:(?:\+|00)45 ?)?(?:[0-9]{2} ?){3}[0-9]{2}(?![0-9])"),  # phone
    re.compile(  # street: "Kildevej 12B", "H.C. Andersens Boulevard 2"
        rf"\b(?:[A-ZÆØÅ][\w.'-]*(?:{_STREET_END})|{_STREET_WORD})\.? +[0-9]{{1,4}}[A-Za-z]?\b"
    ),
    re.compile(r"\b[0-9]{4} +[A-ZÆØÅ][a-zæøå]+(?: +[A-ZÆØÅ]\b)?"),  # postal code + city
)


def scrub_text(value: Any) -> Any:
    """Replace emails, Danish phone numbers and addresses in free text (deep)."""
    if isinstance(value, str):
        for pattern in _TEXT_PATTERNS:
            value = pattern.sub(REDACTED, value)
        return value
    if isinstance(value, dict):
        return {k: scrub_text(v) for k, v in value.items()}
    if isinstance(value, list | tuple):
        return [scrub_text(v) for v in value]
    return value


def _norm(key: str) -> str:
    return key.replace("_", "").replace("-", "").lower()


def scrub(value: Any) -> Any:
    """Return a copy of a JSON-like value with all personal fields replaced."""
    if isinstance(value, dict):
        return {k: _scrub_item(k, v) for k, v in value.items()}
    if isinstance(value, list | tuple):
        return [scrub(v) for v in value]
    return value


def _scrub_item(key: Any, value: Any) -> Any:
    norm = _norm(key) if isinstance(key, str) else None
    if norm in PERSONAL_KEYS:
        return REDACTED
    if norm in FREE_TEXT_KEYS:
        return scrub_text(scrub(value))
    return scrub(value)


def jsonable(value: Any) -> Any:
    """Store models (dataclasses, Decimal, dates) as plain JSON values, before scrub()."""
    if dataclasses.is_dataclass(value) and not isinstance(value, type):
        return jsonable(dataclasses.asdict(value))
    if isinstance(value, dict):
        return {k: jsonable(v) for k, v in value.items()}
    if isinstance(value, list | tuple):
        return [jsonable(v) for v in value]
    if isinstance(value, Decimal):  # money: always "39.00", whatever the store sent
        try:  # sub-øre values (a unit price) are rounded on purpose: docs/adr/0002
            return str(value.quantize(Decimal("0.01"), rounding=ROUND_HALF_UP))
        except InvalidOperation:  # NaN or absurdly large: show what we have, never fail
            return str(value)
    if isinstance(value, datetime | date):
        return value.isoformat()
    return value
