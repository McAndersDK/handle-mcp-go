"""No personal data leaves the server, whatever a store adapter returns."""

import dataclasses
import json
from decimal import Decimal

import pytest

from handle_mcp import server
from handle_mcp.privacy import REDACTED, jsonable, scrub
from tests.conftest import LEAK

CALLS = [
    ("search_products", {"query": "mælk", "stores": ["fake"]}),
    ("list_offers", {"stores": ["fake"]}),
    ("search_recipes", {"query": "karry", "stores": ["fake"]}),
    ("get_recipe", {"store": "fake", "recipe": "r1", "persons": 6}),
    ("list_recipes", {"stores": ["fake"]}),
    ("list_recipes", {"stores": ["fake"], "tags": ["Familievenlig"]}),
    ("compare_basket", {"items": ["mælk 1 l", "2 x rugbrød"], "stores": ["fake"]}),
    ("get_cart", {"store": "fake"}),
    ("set_cart_quantities", {"store": "fake", "items": {"1": 1}}),
    ("add_recipe_to_cart", {"store": "fake", "recipe": "r1"}),
    ("get_delivery_info", {"store": "fake"}),
    ("list_delivery_slots", {"stores": ["fake"]}),
    ("get_orders", {"store": "fake"}),
    ("get_order", {"store": "fake", "order_id": "900000000001"}),
]


def _secrets(value):
    if isinstance(value, dict):
        for v in value.values():
            yield from _secrets(v)
    else:
        yield value


async def test_every_tool_is_checked():
    tested = {name for name, _ in CALLS}
    assert tested == {t.name for t in await server.mcp.list_tools()}


@pytest.mark.parametrize("tool, args", CALLS)
async def test_no_personal_data_in_tool_answers(tool, args):
    text = json.dumps(await getattr(server, tool)(**args), ensure_ascii=False)
    for secret in _secrets(LEAK):
        assert secret not in text, f"{tool} leaked {secret!r}"
    assert REDACTED in text


def test_scrub_nemlig_basket():
    basket = {
        "TotalPrice": 249.87,
        "Lines": [{"Id": "701025", "Name": "Kakaomælk", "Quantity": 2}],
        "DeliveryAddress": {"Name": "Anders And", "StreetName": "Vesterbrogade"},
        "InvoiceAddress": {"PostalCode": "1620"},
        "Email": "a@example.com",
        "DoorCode": "1234",
    }
    out = scrub(basket)
    assert out["Lines"] == basket["Lines"]  # product names stay
    assert out["TotalPrice"] == 249.87
    for key in ("DeliveryAddress", "InvoiceAddress", "Email", "DoorCode"):
        assert out[key] == REDACTED


def test_scrub_bilka_cart_matches_any_case():
    cart = {
        "stat": {"price": 12700},
        "uid": 123456,
        "tid": "abc",
        "deliveryAddress": {"recipient_name": "Anders", "zip": "7100"},
        "orders": [{"order_pickup_code": "87654321", "phone_number": "12345678"}],
    }
    out = scrub(cart)
    assert out["stat"] == {"price": 12700}
    assert out["uid"] == out["tid"] == out["deliveryAddress"] == REDACTED
    assert out["orders"][0] == {"order_pickup_code": REDACTED, "phone_number": REDACTED}


def test_scrub_does_not_change_input():
    data = {"Email": "a@example.com"}
    scrub(data)
    assert data == {"Email": "a@example.com"}


def test_scrub_dawa_address_id():
    cart = {"dawa_uid": "0a3f50a0-0000-0000-0000-000000000000", "stat": {"price": 1}}
    out = scrub(cart)
    assert out["dawa_uid"] == REDACTED
    assert out["stat"] == {"price": 1}


@pytest.mark.parametrize(
    ("value", "text"),
    [
        (Decimal(0), "0.00"),
        (Decimal("0.0"), "0.00"),
        (Decimal(39), "39.00"),
        (Decimal("12.95"), "12.95"),
    ],
)
def test_money_is_always_two_decimals(value, text):
    """nemlig sends "0.0" and "32.0", BilkaToGo "0" and "39.00": one format across stores."""
    assert jsonable({"price": value}) == {"price": text}


def test_money_is_two_decimals_when_nested_and_rounded_half_up():
    @dataclasses.dataclass
    class Line:
        unit_price: Decimal

    out = jsonable({"fees": {"levering": Decimal(39)}, "lines": [Line(Decimal("0.125"))]})
    assert out == {"fees": {"levering": "39.00"}, "lines": [{"unit_price": "0.13"}]}


def test_money_that_cannot_be_quantized_is_still_shown():
    assert jsonable(Decimal("NaN")) == "NaN"
    assert jsonable(Decimal("1e40")) == "1E+40"
