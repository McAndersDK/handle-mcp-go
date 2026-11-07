"""privacy goldens: scrub, scrub_text, the key sets and the money format of jsonable."""

from __future__ import annotations

import copy
import dataclasses
from datetime import UTC, date, datetime
from decimal import Decimal
from zoneinfo import ZoneInfo

from handle_mcp.models import RecipeDetails, RecipeSection
from handle_mcp.privacy import FREE_TEXT_KEYS, PERSONAL_KEYS, REDACTED, jsonable, scrub, scrub_text
from scripts.golden_io import dump

# Every case of tests/test_privacy.py::test_scrub_text_*, then the adversarial inputs.
TEXTS = [
    "Kontakt anders@example.com om ordren",
    "Ring på +45 98 76 54 32",
    "Ring på 98765432",
    "Levering til Kildevej 12B",
    "Levering til H.C. Andersens Boulevard 2",
    "2100 København Ø",
    "Du mangler 123,45 kr. for at nå minimumsbeløbet",
    "Kildevand 0,5 l 24 stk",
    "Max 6 stk. pr. ordre",
    "Varen 5064654 er udsolgt",
    "EAN 5701234567890",
    "Bestil senest 04/11 kl. 23:59",
    "Varen 12345678 er udsolgt",
    "Max 1000 Stk pr. ordre",
    "Ring til os på 98765432",
    "Adresse: Kildevej 12, 2100 København",
    "Sendt til anders@example.com",
    "Ring 98765432",
    "a@b.dk",
    "Ordren til Kildevej 12, 2100 København er sendt til a@b.dk",
    "Kunden 98765432 findes ikke",
    # adversarial
    "123456789",
    "12345678 9",
    "0045 98 76 54 32",
    "Ærøvej 12",
    "Østergade 3B",
    "Søndre Allé 4",
    "blåbær@mælk.dk",
    "2100 København Ø",
    "1000 Stk",
    "+4598765432",
    "45 98 76 54 32",
    "98 76 54 32",
    "9876 5432",
    "98-76-54-32",
    "1234567",
    "٩٨٧٦٥٤٣٢",  # Arabic-Indic digits
    "a.b+c@d-e.co.uk",
    "user@localhost",
    "@",
    "Kildevej 12 B",
    "Kildevej 12",
    "kildevej 12",
    "Vej 5",
    "vej 5",
    "Gade 3.",
    "Boulevard 10-12",
    "Torv 1234",
    "Torv 12345",
    "Vænget 7",
    "Stensparken 3",
    "Møllehaven 2A, 2. th.",
    "2100  København",
    "2100 københavn",
    "9999 Eksempelby",
    "Ring 12 34 56 78 eller 87 65 43 21",
    "tlf: 12345678.",
    "ABC12345678",
    "12345678ABC",
    "Ordre 123456789012",
    "Åløkke Alle 4",
    "Strøget 12",
    "Bredgade 12 og Nyhavn 3",
    "Kildevej 12",
    "2100 København",
    "",
    " ",
    "kr. 1000 København",
]

VALUES = ["x", "", None, 5, ["a"], {"k": "v"}]


def _variants(key: str) -> list[str]:
    mid = len(key) // 2
    return [key, key.upper(), key.title(), key[:mid] + "_" + key[mid:], key[:mid] + "-" + key[mid:]]


def scrub_cases() -> list[dict]:
    basket = {
        "TotalPrice": 249.87,
        "Lines": [{"Id": "701025", "Name": "Kakaomælk", "Quantity": 2}],
        "DeliveryAddress": {"Name": "Anders And", "StreetName": "Vesterbrogade"},
        "InvoiceAddress": {"PostalCode": "1620"},
        "Email": "a@example.com",
        "DoorCode": "1234",
    }
    cart = {
        "stat": {"price": 12700},
        "uid": 123456,
        "tid": "abc",
        "deliveryAddress": {"recipient_name": "Anders", "zip": "0000"},
        "orders": [{"order_pickup_code": "87654321", "phone_number": "12345678"}],
    }
    free = {
        "name": "Mælk 1 l, Kildevej Mejeri",
        "warnings": ["Ring til os på 98765432", "Adresse: Kildevej 12, 2100 København"],
        "results": [{"message": "Sendt til anders@example.com"}],
    }
    dawa = {"dawa_uid": "0a3f50a0-0000-0000-0000-000000000000", "stat": {"price": 1}}
    inputs = {
        "nemlig_basket": basket,
        "bilka_cart": cart,
        "dawa_address_id": dawa,
        "free_text_under_harmless_keys": free,
        "free_text_in_dicts": {
            "messages": [{"text": "Ring 98765432"}],
            "warnings": {"d": "a@b.dk"},
        },
        "notes_key_python_does_not_scrub": {"notes": "Ring 98765432", "note": "Ring 98765432"},
        "tuple_and_nested_lists": {"a": ({"email": "x"}, [{"City": "y"}]), "b": [[{"zip": 1}]]},
        "top_level_list": [{"phone": "1"}, {"name": "ok"}],
        "scalars_pass": {"n": 1, "f": 1.5, "t": True, "z": None, "s": "Kildevej 12"},
        "empty": {},
    }
    out = []
    for name, value in inputs.items():
        before = copy.deepcopy(value)
        result = scrub(value)
        out.append(
            {"name": name, "input": value, "output": result, "input_unchanged": value == before}
        )
    for value in ("x", 5, None, ["Kildevej 12"], ("a", {"email": "b"})):
        out.append(
            {"name": f"top_level_{type(value).__name__}", "input": value, "output": scrub(value)}
        )
    return out


def key_cases() -> dict:
    cases = {}
    for key in sorted(PERSONAL_KEYS | FREE_TEXT_KEYS | {"name", "notes", "id"}):
        cases[key] = {variant: [scrub({variant: v}) for v in VALUES] for variant in _variants(key)}
    return {
        "personal_keys": sorted(PERSONAL_KEYS),
        "free_text_keys": sorted(FREE_TEXT_KEYS),
        "redacted": REDACTED,
        "values": VALUES,
        "scrub_by_key": cases,
    }


def money_cases() -> dict:
    texts = [
        "0",
        "0.0",
        "0.00",
        "-0",
        "39",
        "39.00",
        "12.95",
        "0.125",
        "0.135",
        "12.995",
        "0.005",
        "0.015",
        "0.025",
        "1.005",
        "-0.005",
        "-0.125",
        "-0.126",
        "-1.005",
        "0.004999",
        "1E+2",
        "1e-7",
        "1E+40",
        "NaN",
        "sNaN",
        "Infinity",
        "-Infinity",
        "9999999999999999999999.995",
        "99999999999999999999999999.99",
        "999999999999999999999999999.99",
        "123456789012345678901234567890.125",
        "0.1234567890123456789",
        "1234.5",
        "7",
        "100",
    ]
    dk = ZoneInfo("Europe/Copenhagen")
    stamps = [
        datetime(2026, 11, 3, 16),
        datetime(2026, 11, 3, 16, 0, 0, 5),
        datetime(2026, 11, 3, 16, tzinfo=UTC),
        datetime(2026, 3, 29, 2, 30, tzinfo=dk),
        datetime(2026, 10, 25, 2, 30, tzinfo=dk),
        datetime(2026, 10, 25, 2, 30, tzinfo=dk, fold=1),
    ]

    @dataclasses.dataclass
    class Line:
        unit_price: Decimal
        when: date | None = None

    nested = {
        "fees": {"levering": Decimal(39), "poser": Decimal("0.0")},
        "lines": [Line(Decimal("0.125")), Line(Decimal("12.995"), date(2026, 11, 2))],
        "tuple": (Decimal(1), "x", None),
        "flags": [True, False, None, 3, 1.5, 249.87],
    }
    recipe = RecipeDetails(
        "nemlig",
        "kebab",
        "Kebabfad",
        "https://example.com/x",
        4,
        description="Prøv denne lækre opskrift",
        image="https://media.example.com/a.jpg",
        method=[RecipeSection("Kebabfad", ["Tænd ovnen på 200 grader"])],
        tip="Server med citron",
    )
    return {
        "decimals": [{"input": t, "output": jsonable(Decimal(t))} for t in texts],
        "datetimes": [
            {"input": d.isoformat(), "fold": d.fold, "output": jsonable(d)} for d in stamps
        ]
        + [{"input": "2026-11-02", "output": jsonable(date(2026, 11, 2))}],
        "nested": {"output": jsonable(nested)},
        "recipe_text_is_unchanged": {
            "output": jsonable(recipe),
            "scrubbed": scrub(jsonable(recipe)),
        },
    }


def export() -> dict[str, int]:
    sizes = {"privacy/scrub.json": dump("privacy/scrub.json", {"cases": scrub_cases()})}
    strings = [{"input": t, "output": scrub_text(t)} for t in TEXTS]
    deep = {"a": ["Ring 98765432", ("a@b.dk",)], "b": {"c": "Kildevej 12"}, "n": 5, "z": None}
    structures = [
        {"input": deep, "output": scrub_text(deep)},
        {"input": 5, "output": scrub_text(5)},
    ]
    body = {"redacted": REDACTED, "strings": strings, "structures": structures}
    sizes["privacy/scrub_text.json"] = dump("privacy/scrub_text.json", body)
    sizes["privacy/keys.json"] = dump("privacy/keys.json", key_cases())
    sizes["privacy/money.json"] = dump("privacy/money.json", money_cases())
    return sizes
