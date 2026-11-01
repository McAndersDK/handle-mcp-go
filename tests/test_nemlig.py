"""nemlig.com adapter against a fake backend (httpx.MockTransport). No network."""

import json
from decimal import Decimal
from pathlib import Path

import httpx
import pytest

from handle_mcp import server
from handle_mcp import stores as registry
from handle_mcp.stores import StoreError
from handle_mcp.stores import nemlig as nemlig_module
from handle_mcp.stores.nemlig import NemligStore

FIXTURES = Path(__file__).parent / "fixtures" / "nemlig"


def fixture(name: str) -> dict:
    return json.loads((FIXTURES / f"{name}.json").read_text())


class FakeNemlig:
    """Answers like nemlig.com. Records every request. Tests can script failures."""

    def __init__(self) -> None:
        self.requests: list[httpx.Request] = []
        self.basket = fixture("basket")
        self.scripted: dict[str, list] = {}  # path → status codes or errors to give first
        self.token_count = 0
        self.queue = False

    def handle(self, request: httpx.Request) -> httpx.Response:
        self.requests.append(request)
        path = request.url.path
        if self.scripted.get(path):
            nxt = self.scripted[path].pop(0)
            if isinstance(nxt, Exception):
                raise nxt
            return httpx.Response(nxt)
        if request.url.host == "nemlig.queue-it.net":
            return httpx.Response(200, text="<html>Du er i kø</html>")
        if path == "/" and self.queue:
            return httpx.Response(
                302, headers={"Location": "https://nemlig.queue-it.net/?c=nemlig"}
            )
        if path == "/webapi/AntiForgery":
            return httpx.Response(200, json={"Header": "X-XSRF-TOKEN", "Value": "xsrf-1"})
        if path == "/webapi/Token":
            self.token_count += 1
            return httpx.Response(
                200, json={"access_token": f"bearer-{self.token_count}", "expires_in": 300}
            )
        if path == "/webapi/login":
            body = json.loads(request.content)
            if body["Password"] != "hemmeligt":
                return httpx.Response(401)
            return httpx.Response(200, json={"RedirectUrl": "/", "TimeslotUtc": "x"})
        if path == "/webapi/v2/AppSettings/Website":
            return httpx.Response(200, json={"CombinedProductsAndSitecoreTimestamp": "ts-1"})
        if path == "/" and request.url.params.get("GetAsJson"):
            return httpx.Response(
                200, json={"Settings": {"TimeslotUtc": "2026110316-60-180", "DeliveryZoneId": 3}}
            )
        if path == "/searchgateway/api/search":
            return httpx.Response(200, json=fixture("search"))
        if path == "/webapi/basket/GetBasket":
            return httpx.Response(200, json=self.basket)
        if path == "/webapi/basket/AddToBasket":
            body = json.loads(request.content)
            pid, qty = body["ProductId"], body["quantity"]
            if pid == "sold-out":
                qty = 0  # nemlig quietly refuses
            lines = [ln for ln in self.basket["Lines"] if ln["Id"] != pid]
            if qty:
                lines.append({"Id": pid, "Name": "Vare", "Quantity": qty, "Price": 10.0 * qty})
            self.basket = {**self.basket, "Lines": lines}
            return httpx.Response(200, json=self.basket)
        if path == "/webapi/Order/DeliverySpot":
            return httpx.Response(200, json=fixture("delivery_spot"))
        if path == "/webapi/order/GetBasicOrderHistory":
            return httpx.Response(200, json=fixture("order_history"))
        if path.startswith("/webapi/v2/order/GetOrderHistory/"):
            return httpx.Response(200, json=fixture("order_details"))
        return httpx.Response(404)

    def paths(self) -> list[str]:
        return [r.url.path for r in self.requests]


@pytest.fixture
def fake(monkeypatch):
    monkeypatch.setenv("HANDLE_NEMLIG_EMAIL", "kunde@example.com")
    monkeypatch.setenv("HANDLE_NEMLIG_PASSWORD", "hemmeligt")
    monkeypatch.setattr(nemlig_module.asyncio, "sleep", _no_sleep)
    return FakeNemlig()


async def _no_sleep(_seconds):
    return None


@pytest.fixture
def store(fake):
    return NemligStore(
        httpx.AsyncClient(transport=httpx.MockTransport(fake.handle), follow_redirects=True)
    )


async def test_login_flow_then_authorized_calls(store, fake):
    await store.get_cart()
    assert fake.paths()[:5] == [
        "/webapi/Token",
        "/webapi/AntiForgery",
        "/webapi/login",
        "/webapi/Token",
        "/webapi/AntiForgery",
    ]
    login = next(r for r in fake.requests if r.url.path == "/webapi/login")
    assert json.loads(login.content)["Username"] == "kunde@example.com"
    assert login.headers["X-XSRF-TOKEN"] == "xsrf-1"
    basket = fake.requests[-1]
    assert basket.headers["Authorization"] == "Bearer bearer-2"  # fresh token after login


async def test_logs_in_only_once(store, fake):
    await store.get_cart()
    await store.get_cart()
    assert fake.paths().count("/webapi/login") == 1


async def test_wrong_password_is_a_clear_error(store, fake, monkeypatch):
    monkeypatch.setenv("HANDLE_NEMLIG_PASSWORD", "forkert")
    with pytest.raises(StoreError, match="Tjek email og kodeord"):
        await store.get_cart()


async def test_expired_token_is_renewed_once(store, fake):
    await store.get_cart()
    fake.scripted["/webapi/basket/GetBasket"] = [401]
    cart = await store.get_cart()
    assert cart.total == Decimal("182.45")
    assert fake.paths().count("/webapi/login") == 1  # token renewal was enough


async def test_retries_on_503_then_succeeds(store, fake):
    fake.scripted["/webapi/basket/GetBasket"] = [503, 503]
    assert (await store.get_cart()).lines


async def test_retries_on_network_error(store, fake):
    """Seen live: nemlig sometimes drops the connection (ReadError) during many searches."""
    await store.get_cart()
    fake.scripted["/webapi/basket/GetBasket"] = [httpx.ReadError("reset"), 503]
    assert (await store.get_cart()).lines


async def test_gives_up_after_retries(store, fake):
    fake.scripted["/webapi/basket/GetBasket"] = [503, 503, 503]
    with pytest.raises(StoreError, match="503"):
        await store.get_cart()


async def test_no_retry_on_400(store, fake):
    await store.get_cart()
    before = len(fake.requests)
    fake.scripted["/webapi/basket/GetBasket"] = [400]
    with pytest.raises(StoreError, match="400"):
        await store.get_cart()
    assert len(fake.requests) == before + 1


async def test_search_parses_products(store, fake):
    products = await store.search("cocio", limit=5)
    can, bottle = products
    assert (can.id, can.price, can.unit_price, can.unit, can.size) == (
        "5070417",
        Decimal("119.0"),
        Decimal("9.92"),
        "stk",
        "12 x 0,25 l / Classic",
    )
    assert not can.on_offer and can.available and can.ean is None
    assert can.category == "Drikke"
    assert bottle.unit == "l" and bottle.on_offer and bottle.offer_text == "3 for 68.0 kr"
    assert not bottle.available
    search = next(r for r in fake.requests if r.url.path == "/searchgateway/api/search")
    assert search.url.params["timeslotUtc"] == "2026110316-60-180"
    assert search.url.params["deliveryZoneId"] == "3"


async def test_recipes_are_parsed_with_full_links(store, fake):
    (recipe,) = await store.recipes("cocio", limit=3)
    assert (recipe.name, recipe.time, recipe.persons) == ("Cocioshake med topping", "10 min", 4)
    assert recipe.url == "https://www.nemlig.com/opskrifter/cocioshake-topping-98003783"
    search = next(r for r in fake.requests if r.url.path == "/searchgateway/api/search")
    assert search.url.params["recipeCount"] == "3"


async def test_search_reuses_settings(store, fake):
    await store.search("mælk")
    await store.search("brød")
    assert fake.paths().count("/webapi/v2/AppSettings/Website") == 1


async def test_cart_has_fees_minimum_and_warnings(store):
    cart = await store.get_cart()
    assert [(ln.product_id, ln.quantity) for ln in cart.lines] == [("5064654", 1), ("701025", 2)]
    assert cart.products_total == Decimal("147.45")
    assert cart.fees == {"levering": Decimal("29.0"), "poser": Decimal("6.0")}
    assert cart.total == Decimal("182.45")
    assert (cart.minimum_order, cart.minimum_met) == (Decimal("300.0"), False)
    assert cart.warnings == ["Minimumskøb er 300 kr."]
    assert cart.delivery == "Tirsdag 3. nov. kl. 17-20"


async def test_set_quantities_absolute_and_remove(store, fake):
    results = await store.set_quantities({"701025": 5, "5064654": 0})
    assert [(r.product_id, r.applied, r.ok) for r in results] == [
        ("701025", 5, True),
        ("5064654", 0, True),
    ]
    sent = [json.loads(r.content) for r in fake.requests if r.url.path.endswith("AddToBasket")]
    assert [(b["ProductId"], b["quantity"]) for b in sent] == [("701025", 5), ("5064654", 0)]


async def test_set_quantities_reports_refusal(store):
    [result] = await store.set_quantities({"sold-out": 2})
    assert (result.applied, result.ok) == (0, False)
    assert "ikke 2" in result.message


async def test_set_quantities_one_failure_does_not_stop_the_rest(store, fake):
    await store.get_cart()
    fake.scripted["/webapi/basket/AddToBasket"] = [400]
    results = await store.set_quantities({"1": 1, "2": 1})
    assert [r.ok for r in results] == [False, True]


async def test_terms_and_delivery_info(store):
    terms = await store.terms()
    assert (terms.home_delivery, terms.delivery_fee, terms.minimum_order) == (
        True,
        Decimal("29.0"),
        Decimal("300.0"),
    )
    info = await store.delivery_info()
    assert info.chosen_start.isoformat() == "2026-11-03T16:00:00+00:00"
    assert info.edit_deadline.isoformat() == "2026-11-02T23:00:00+00:00"
    assert "17-20" in info.note


async def test_orders_and_order(store):
    [order] = await store.orders(limit=5)
    assert (order.id, order.status, order.total) == ("900001", "Leveret", Decimal("612.5"))
    assert order.delivery_start.hour == 15
    detail = await store.order("900001")
    assert [(ln.product_id, ln.quantity) for ln in detail.lines] == [("5064654", 1), ("701025", 3)]
    assert detail.total == Decimal("167.95")  # sum of lines when the answer has no total


async def test_never_calls_order_or_payment_endpoints(store, fake):
    await store.search("mælk")
    await store.get_cart()
    await store.set_quantities({"1": 1})
    await store.terms()
    await store.delivery_info()
    await store.orders()
    await store.order("900001")
    assert not [p for p in fake.paths() if "Checkout" in p or "PlaceOrder" in p]


async def test_tools_hide_personal_data_from_nemlig(store, monkeypatch):
    monkeypatch.setattr(registry, "STORES", {"nemlig": NemligStore})
    monkeypatch.setattr(server, "_instances", {"nemlig": store})
    text = json.dumps(
        [
            await server.get_cart("nemlig"),
            await server.get_delivery_info("nemlig"),
            await server.get_orders("nemlig"),
            await server.get_order("nemlig", "900001"),
        ],
        ensure_ascii=False,
    )
    for secret in (
        "Kunde Eksempel",
        "Testvej",
        "kunde@example.com",
        "kode-4711",
        "bagdøren",
        "98765432",
    ):
        assert secret not in text
    assert "Black Coffee Blend Supreme" in text


async def test_queue_it_waiting_room_is_a_clear_error(store, fake):
    fake.queue = True
    with pytest.raises(StoreError, match="kø"):
        await store.search("mælk")


async def test_recipes_format_change_is_a_clear_error(store, monkeypatch):
    async def no_recipes(*_args, **_kwargs):
        return {"Products": {"Products": []}}

    async def settings():
        return {}

    monkeypatch.setattr(store, "_api", no_recipes)
    monkeypatch.setattr(store, "_search_settings", settings)
    with pytest.raises(StoreError, match="ændret format"):
        await store.recipes("karry")


async def test_recipe_links_absolute_or_empty(store, monkeypatch):
    async def recipes(*_args, **_kwargs):
        return {
            "Recipes": [
                {"Id": "a", "Name": "Absolut", "Url": "https://www.nemlig.com/opskrifter/a-1"},
                {"Id": "b", "Name": "Uden link", "Url": ""},
            ]
        }

    async def settings():
        return {}

    monkeypatch.setattr(store, "_api", recipes)
    monkeypatch.setattr(store, "_search_settings", settings)
    (only,) = await store.recipes("x")
    assert only.url == "https://www.nemlig.com/opskrifter/a-1"
