"""Strip personal data before anything leaves the server.

Every tool answer goes through `scrub()`. Whatever a tool returns lands in the
model's context and the chat transcript, so name, address, phone, email, door
code and delivery notes must never be in it.

Values are replaced with a marker instead of deleted, so the model can still
tell that an order *had* a delivery address. Keys are matched without case,
`_` or `-`, so `doorCode`, `DoorCode` and `door_code` are the same key.
"""

from __future__ import annotations

import dataclasses
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


def _norm(key: str) -> str:
    return key.replace("_", "").replace("-", "").lower()


def scrub(value: Any) -> Any:
    """Return a copy of a JSON-like value with all personal fields replaced."""
    if isinstance(value, dict):
        return {
            k: REDACTED if isinstance(k, str) and _norm(k) in PERSONAL_KEYS else scrub(v)
            for k, v in value.items()
        }
    if isinstance(value, list | tuple):
        return [scrub(v) for v in value]
    return value


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
