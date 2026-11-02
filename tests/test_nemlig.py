"""nemlig.com adapter against a fake backend (httpx.MockTransport). No network."""

import json
from datetime import date
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


OFFER_FILES = {
    "g-skarp": "offers_skarp_pris",
    "g-priskup": "offers_priskup",
    "g-dag": "offers_dagstilbud",
    "g-madspild": "offers_madspild",
    "g-gave": "offers_gratis",
}
TILBUD = [  # the spots on /tilbud?GetAsJson=1, trimmed (tests/fixtures/nemlig/README.md)
    {"TemplateName": "productlistshowallspot", "Heading": "Skarp pris", "ProductGroupId": "g-skarp"},
    {"TemplateName": "productlistshowallspot", "Heading": "Ugens Priskup", "ProductGroupId": "g-priskup"},
    {"TemplateName": "productlistshowallspot", "Heading": "Dagstilbud", "ProductGroupId": "g-dag"},
    {"TemplateName": "productlistshowallspot", "Heading": "Undgå madspild", "ProductGroupId": "g-madspild"},
    {"TemplateName": "productlistshowallspot", "Heading": "En gave til dig", "ProductGroupId": "g-gave"},
    {"TemplateName": "productlistshowallspot", "Heading": "Nyheder", "ProductGroupId": "g-news"},
    {"TemplateName": "bannerspot", "Heading": "Skarp pris"},
]  # fmt: skip


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
                200,
                json={
                    "Settings": {
                        "TimeslotUtc": "2026110316-60-180",
                        "DeliveryZoneId": 3,
                        "UserId": "user-4711",
                    }
                },
            )
        if path == "/searchgateway/api/search":
            if request.url.params.get("query") in ("mums rosinboller", "gratis"):
                return httpx.Response(200, json=fixture("search_gift"))
            return httpx.Response(200, json=fixture("search"))
        if path == "/tilbud":
            return httpx.Response(200, json={"content": TILBUD})
        if path.endswith("/Products/GetByProductGroupId"):
            return httpx.Response(
                200, json=fixture(OFFER_FILES[request.url.params["productGroupId"]])
            )
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
    assert bottle.unit == "l" and bottle.on_offer and bottle.offer_text == "3 for 68 kr"
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


async def test_cart_hides_removed_lines(store, fake):
    fake.basket = {**fake.basket, "Lines": [*fake.basket["Lines"], {"Id": "9", "Name": "Æg",
        "Quantity": 0, "Price": 28.0}]}  # fmt: skip
    cart = await store.get_cart()
    assert "9" not in [ln.product_id for ln in cart.lines]
    assert cart.products_total == Decimal("147.45")  # nemlig's own total is kept


async def test_terms_estimate_bags_for_an_empty_cart(store, fake, monkeypatch):
    assert (await store.terms()).other_fees == {"poser": Decimal("6.0")}
    fake.basket = {**fake.basket, "TotalBagsPrice": 0}  # products but no bag fee: no guess
    assert (await store.terms()).other_fees == {}
    fake.basket = {**fake.basket, "Lines": [], "TotalBagsPrice": 0}
    assert (await store.terms()).other_fees == {"poser (anslået)": Decimal("17.85")}
    monkeypatch.setenv("HANDLE_NEMLIG_BAG_FEE", "12,50")
    assert (await store.terms()).other_fees == {"poser (anslået)": Decimal("12.50")}
    monkeypatch.setenv("HANDLE_NEMLIG_BAG_FEE", "ikke et tal")
    assert (await store.terms()).other_fees == {"poser (anslået)": Decimal("17.85")}


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


# Shapes seen live 2026-11-01 (docs/research.md). Only the fields we read.
def test_discount_campaign_pays_the_campaign_price(store):
    hit = {
        "Id": 1,
        "Name": "Laksefilet m. skind",
        "Price": 120.75,
        "UnitPriceCalc": 201.25,
        "Campaign": {
            "Type": "ProductCampaignDiscountPercent",
            "DiscountPercent": 30,
            "CampaignPrice": 84.53,
            "CampaignUnitPrice": 140.88,
            "IntervalEnd": "2026-11-04T22:59:59Z",
        },
    }
    p = store._product(hit)
    assert (p.price, p.unit_price, p.before_price) == (
        Decimal("84.53"),
        Decimal("140.88"),
        Decimal("120.75"),
    )
    assert p.on_offer and p.offer_text == "-30 %"
    assert p.offer_until == date(2026, 11, 4)  # 22:59:59 UTC is the last minute of the day in DK


def test_discount_label_alone_is_not_an_offer(store):
    hit = {"Id": 2, "Name": "Letmælk 1,5%", "Price": 10.95, "Campaign": None, "DiscountItem": True}
    p = store._product(hit)
    assert (p.price, p.on_offer, p.offer_text, p.before_price) == (
        Decimal("10.95"),
        False,
        None,
        None,
    )


def test_multi_buy_keeps_the_single_price(store):
    campaign = {"Type": "ProductCampaignMixOffer", "MinQuantity": 4, "TotalPrice": 50.0}
    campaign["CampaignPrice"] = 50.0
    p = store._product({"Id": 3, "Name": "Hakkede tomater", "Price": 13.95, "Campaign": campaign})
    assert (p.price, p.offer_text, p.before_price) == (Decimal("13.95"), "4 for 50 kr", None)


def test_campaign_we_cannot_describe_is_still_an_offer(store):
    campaign = {"Type": "ProductCampaignBuyXForY", "MinQuantity": 3}  # no TotalPrice
    p = store._product({"Id": 4, "Name": "Letmælk", "Price": 21.28, "Campaign": campaign})
    assert (p.price, p.on_offer, p.offer_text) == (Decimal("21.28"), True, "Tilbud")


def test_plain_discount_says_the_before_price(store):
    campaign = {"Type": "ProductCampaignDiscount", "CampaignPrice": 79.95}
    p = store._product({"Id": 5, "Name": "Laks - back loin", "Price": 98.75, "Campaign": campaign})
    assert p.offer_text == "Tilbud, før 98,75 kr"


def test_multi_buy_text_is_priced_by_compare(store):
    from handle_mcp.compare import best_match

    campaign = {"Type": "ProductCampaignMixOffer", "MinQuantity": 4, "TotalPrice": 50.0}
    hit = {"Id": 6, "Name": "Hakkede tomater", "Price": 13.95, "Description": "400 g"}
    p = store._product({**hit, "Campaign": campaign, "Availability": {}})
    assert best_match("4 x hakkede tomater 400 g", [p]).line_total == Decimal(50)


def test_offer_end_uses_danish_time(store):
    summer = {
        "Type": "ProductCampaignDiscount",
        "CampaignPrice": 10,
        "IntervalEnd": "2026-07-01T21:59:59Z",
    }
    p = store._product({"Id": 7, "Name": "Is", "Price": 20, "Campaign": summer})
    assert p.offer_until == date(2026, 7, 1)  # 23:59:59 in DK summer time


async def test_offers_reads_every_offer_list(store, fake):
    offers = await store.offers(10)
    lists = {p.campaign for p in offers}
    assert lists == {"Skarp pris", "Priskup", "Dagstilbud", "Undgå madspild", "Gratis"}
    calls = [r for r in fake.requests if r.url.path.endswith("GetByProductGroupId")]
    assert len(calls) == 5  # "Nyheder" is not an offer list
    assert (
        calls[0].url.path
        == "/webapi/ts-1/2026110316-60-180/3/user-4711/Products/GetByProductGroupId"
    )
    assert calls[0].url.params["pagesize"] == "10"
    assert all(r.method == "GET" for r in fake.requests if r.url.path != "/webapi/login")
    rejer = next(p for p in offers if p.id == "5061889")
    assert (rejer.price, rejer.before_price, rejer.campaign) == (
        Decimal("25.0"),
        Decimal("31.5"),
        "Priskup",
    )


async def test_offer_above_the_normal_price_is_not_an_offer(store):
    hit = next(h for h in fixture("offers_dagstilbud")["Products"] if h["Id"] == "5071238")
    steak = store._product(hit)
    assert (steak.price, steak.before_price, steak.on_offer) == (Decimal("82.565"), None, False)


async def test_free_gift_costs_nothing_and_says_max_one(store):
    [gift] = await store.search("mums rosinboller")
    assert (gift.price, gift.before_price) == (Decimal(0), Decimal("20.45"))
    assert (gift.campaign, gift.max_per_order) == ("Gratis", 1)
    assert gift.offer_text == "Gratis (max 1 stk.)"


async def test_offers_without_lists_is_a_clear_error(store, fake, monkeypatch):
    monkeypatch.setattr(nemlig_module, "OFFER_LISTS", {})
    with pytest.raises(StoreError, match="tilbudslister"):
        await store.offers()


async def test_list_offers_tool_never_shows_the_user_id(store, monkeypatch):
    monkeypatch.setitem(registry.STORES, "nemlig", lambda: store)
    monkeypatch.setattr(server, "_instances", {}, raising=False)
    monkeypatch.setattr(server, "_store", lambda _name: store)
    out = await server.list_offers(["nemlig"], 5)
    text = json.dumps(out, default=str)
    assert "user-4711" not in text and "Priskup" in text


async def test_offer_list_error_never_shows_the_user_id(store, fake):
    url = "/webapi/ts-1/2026110316-60-180/3/user-4711/Products/GetByProductGroupId"
    fake.scripted[url] = [404] * len(TILBUD)  # every list fails
    with pytest.raises(StoreError) as err:
        await store.offers()
    assert "user-4711" not in str(err.value) and "<bruger>" in str(err.value)


async def test_offers_need_a_user(store, monkeypatch):
    await store._search_settings()
    store._user_id = ""
    monkeypatch.setattr(store, "_search_settings", lambda: _settings(store))
    with pytest.raises(StoreError, match="Log ind"):
        await store.offers()


async def _settings(store):
    return store._settings[1]


async def test_one_broken_offer_list_does_not_hide_the_others(store, fake):
    url = "/webapi/ts-1/2026110316-60-180/3/user-4711/Products/GetByProductGroupId"
    fake.scripted[url] = [404]  # the first list (Skarp pris) fails
    offers = await store.offers(10)
    assert "Skarp pris" not in {p.campaign for p in offers} and offers


async def test_offers_leave_out_an_offer_above_the_normal_price(store):
    assert "5071238" not in {p.id for p in await store.offers(10)}


async def test_offers_find_free_gifts_by_search(store, fake):
    """Live 2026-11-02: "En gave til dig" was empty, but search "gratis" found the gift."""
    gift = next(p for p in await store.offers(10) if p.id == "5606321")
    assert (gift.price, gift.campaign, gift.max_per_order) == (Decimal(0), "Gratis", 1)
    searches = [r for r in fake.requests if r.url.path == "/searchgateway/api/search"]
    assert [r.url.params["query"] for r in searches] == ["gratis"]


async def test_offers_survive_a_failed_gift_search(store, fake):
    fake.scripted["/searchgateway/api/search"] = [500] * 3
    offers = await store.offers(10)
    assert offers and "5606321" not in {p.id for p in offers}
