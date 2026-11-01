"""No personal data leaves the server, whatever a store adapter returns."""

import json

import pytest

from handle_mcp import server
from handle_mcp.privacy import REDACTED, scrub
from tests.conftest import LEAK

CALLS = [
    ("search_products", {"query": "mælk", "stores": ["fake"]}),
    ("compare_basket", {"items": ["mælk 1 l", "2 x rugbrød"], "stores": ["fake"]}),
    ("get_cart", {"store": "fake"}),
    ("set_cart_quantities", {"store": "fake", "items": {"1": 1}}),
    ("get_delivery_info", {"store": "fake"}),
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
