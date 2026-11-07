"""BilkaToGo store cases."""

from __future__ import annotations

import httpx

from scripts.golden_fakes import resp
from scripts.golden_hooks import body, cart, expire_jwt, respond, scripted
from scripts.golden_stores import Case
from tests.test_bilkatogo import fixture

C = Case
CART = "/api/shop/v6/Cart"
CHANGE = "/api/shop/v6/ChangeLineCount"
AVAIL = "/api/shop/v3/AvailableDeliveryDates"
ALGOLIA = "/1/indexes/*/queries"
JWT = "/accounts.getJWT"
LOGIN = "/accounts.login"
LOGIN_JWT = "/api/auth/LoginJWT"
HOME = fixture("cart_home")
EXPIRED = {"code": 1, "msg": "Du er ikke logget ind"}
SLOT = HOME["deliveryDate"]
DST_SPRING = [
    ("01:30", "02:30"),
    ("02:00", "03:00"),
    ("02:30", "02:45"),
    ("03:00", "06:00"),
    ("23:00", "01:00"),
]
DST_AUTUMN = [
    ("00:00", "06:00"),
    ("01:00", "02:30"),
    ("02:00", "03:00"),
    ("02:30", "02:45"),
    ("23:00", "01:00"),
]


def pickup(fake, _store) -> None:
    fake.cart = fixture("cart_logged_in")


CART_CASES = [
    C("cart_home_password", "get_cart"),
    C("cart_session", "get_cart", login="session"),
    C("cart_dead_session", "get_cart", login="dead_session"),
    C("cart_wrong_password", "get_cart", login="wrong_password"),
    C("cart_no_login", "get_cart", login=None),
    C("cart_invalid_keychain", "get_cart", login="invalid"),
    C("cart_session_without_token", "get_cart", login="no_token"),
    C(
        "cart_gigya_rate_limit",
        "get_cart",
        login="session",
        setup=scripted(JWT, {"errorCode": 403048, "errorMessage": "busy"}),
    ),
    C(
        "cart_gigya_server_error",
        "get_cart",
        login="session",
        setup=scripted(JWT, {"errorCode": 500001, "errorMessage": "busy"}),
    ),
    C(
        "cart_gigya_other_error",
        "get_cart",
        setup=scripted(LOGIN, {"errorCode": 400006, "errorMessage": "Nej"}),
    ),
    C("cart_gigya_error_without_message", "get_cart", setup=scripted(LOGIN, {"errorCode": 7})),
    C(
        "cart_gigya_error_code_text",
        "get_cart",
        setup=scripted(LOGIN, {"errorCode": "abc", "errorMessage": "x"}),
    ),
    C("cart_gigya_not_json", "get_cart", setup=respond(LOGIN, resp(text="<html>"))),
    C("cart_gigya_503_gives_up", "get_cart", setup=scripted(LOGIN, 503, 503, 503)),
    C(
        "cart_login_without_token",
        "get_cart",
        setup=scripted(LOGIN, {"errorCode": 0, "sessionInfo": {}}),
    ),
    C("cart_login_without_jwt", "get_cart", setup=scripted(JWT, {"errorCode": 0})),
    C("cart_loginjwt_403", "get_cart", setup=scripted(LOGIN_JWT, 403)),
    C("cart_loginjwt_503_gives_up", "get_cart", setup=scripted(LOGIN_JWT, 503, 503, 503)),
    C("cartpickup", "get_cart", setup=pickup),
    C(
        "cart_slot_ended",
        "get_cart",
        setup=cart(deliveryDate={**SLOT, "deliveryDate": "2024-09-04"}),
    ),
    C(
        "cart_slot_ends_today_before_now",
        "get_cart",
        setup=cart(
            deliveryDate={
                **SLOT,
                "deliveryDate": "2026-11-02",
                "intervalStart": "09:00",
                "intervalEnd": "11:59",
            }
        ),
    ),
    C(
        "cart_slot_ends_today_after_now",
        "get_cart",
        setup=cart(
            deliveryDate={
                **SLOT,
                "deliveryDate": "2026-11-02",
                "intervalStart": "11:00",
                "intervalEnd": "12:01",
            }
        ),
    ),
    C("cart_slot_unreadable", "get_cart", setup=cart(deliveryDate={**SLOT, "intervalEnd": None})),
    C("cart_no_slot", "get_cart", setup=cart(deliveryDate=...)),
    C("cart_no_address", "get_cart", setup=cart(deliveryAddress=None)),
    C(
        "cart_messages",
        "get_cart",
        setup=cart(messages=[{"text": "a<b>b</b>"}, "plain", {"nope": 1}]),
    ),
    C("cart_minimal", "get_cart", setup=body(CART, {})),
    C(
        "cart_fees_zero",
        "get_cart",
        setup=cart(stat={"price": 5000, "minimum_left": 0, "delivery_price": 0, "deposit": 250}),
    ),
    C("cart_amount_not_a_number", "get_cart", setup=cart(stat={"price": "abc"})),
    C(
        "cart_amount_decimal_text",
        "get_cart",
        setup=cart(stat={"price": "1250.5", "delivery_price": 3900}),
    ),
    C(
        "cart_expired_body_logs_in_again",
        "get_cart",
        then=[("get_cart", (), scripted(CART, EXPIRED))],
    ),
    C("cart_expired_twice", "get_cart", then=[("get_cart", (), scripted(CART, EXPIRED, EXPIRED))]),
    C(
        "cart_other_error_body",
        "get_cart",
        then=[
            ("get_cart", (), scripted(CART, {"code": None, "msg": "Noget gik galt", "eid": 500}))
        ],
    ),
    C("cart_401_logs_in_again", "get_cart", then=[("get_cart", (), scripted(CART, 401))]),
    C("cart_401_twice", "get_cart", then=[("get_cart", (), scripted(CART, 401, 401))]),
    C("cart_503_twice", "get_cart", setup=scripted(CART, 503, 503)),
    C("cart_503_gives_up", "get_cart", setup=scripted(CART, 503, 503, 503)),
    C(
        "cart_network_errors",
        "get_cart",
        setup=lambda f, s: f.scripted.update(
            {CART: [httpx.ReadError("reset"), httpx.RemoteProtocolError("closed")]}
        ),
    ),
    C(
        "cart_timeout_gives_up",
        "get_cart",
        setup=lambda f, s: f.scripted.update({CART: [httpx.ConnectTimeout("t") for _ in range(3)]}),
    ),
    C(
        "cart_other_http_error",
        "get_cart",
        setup=lambda f, s: f.scripted.update({CART: [httpx.UnsupportedProtocol("x")]}),
    ),
    C("cart_400", "get_cart", setup=scripted(CART, 400)),
    C("cart_not_json", "get_cart", setup=respond(CART, resp(text="<html>"))),
    C("cart_json_list", "get_cart", setup=body(CART, [1])),
    C("cart_jwt_expires_logs_in_again", "get_cart", then=[("get_cart", (), expire_jwt)]),
    C("cart_logs_in_once", "get_cart", then=[("get_cart", (), None)]),
    C("session_before_login", "session", login="session"),
]

SEARCH_CASES = [
    C("search_skyr", "search", ("skyr", 5)),
    C("search_default_limit", "search", ("mælk",)),
    C("search_session", "search", ("skyr", 5), login="session"),
    C(
        "search_sid_9001",
        "search",
        ("skyr", 5),
        setup=cart(deliveryAddress={**HOME["deliveryAddress"], "sid": "9001"}),
    ),
    C("search_no_login", "search", ("mælk",), login=None),
    C("search_no_address", "search", ("mælk",), setup=cart(deliveryAddress=None)),
    C("search_address_without_sid", "search", ("mælk",), setup=cart(deliveryAddress={"sid": None})),
    C("search_fior_di_latte", "search", ("fior di latte", 50)),
    C("search_mozzarella", "search", ("mozzarella", 50)),
    C("search_hvidloeg", "search", ("hvidløg", 50)),
    C("search_makrel_tomat", "search", ("makrel i tomat", 50)),
    C("search_hakkede_tomater", "search", ("hakkede tomater", 50)),
    C("search_403", "search", ("mælk",), setup=scripted(ALGOLIA, 403)),
    C("search_503_gives_up", "search", ("mælk",), setup=scripted(ALGOLIA, 503, 503, 503)),
    C("search_format_changed", "search", ("mælk",), setup=body(ALGOLIA, {"results": []})),
    C("search_not_json", "search", ("mælk",), setup=respond(ALGOLIA, resp(text="<html>"))),
    C(
        "search_hit_without_store_data",
        "search",
        ("mælk",),
        setup=body(ALGOLIA, {"results": [{"hits": [{"objectID": "1", "name": "A"}]}]}),
    ),
    C("offers_default", "offers", (10,)),
    C("offers_default_limit", "offers"),
    C(
        "offers_sid_9001",
        "offers",
        (10,),
        setup=cart(deliveryAddress={**HOME["deliveryAddress"], "sid": "9001"}),
    ),
    C("offers_no_login", "offers", (10,), login=None),
]

SET_CASES = [
    C("set_ok_refused_limited", "set_quantities", ({"18381": 3, "88672": 1, "limited": 9},)),
    C(
        "set_refused_by_message_body",
        "set_quantities",
        ({"1": 1, "2": 2},),
        setup=scripted(CHANGE, {"code": None, "msg": "Nej tak"}),
    ),
    C(
        "set_store_failure_stops",
        "set_quantities",
        ({"18381": 2, "19753": 1},),
        setup=scripted(CHANGE, 503, 503, 503),
    ),
    C(
        "set_applied_is_text",
        "set_quantities",
        ({"1": 3},),
        setup=scripted(CHANGE, {"productAmount": "3", "message": ""}),
    ),
    C(
        "set_applied_missing",
        "set_quantities",
        ({"1": 3},),
        setup=scripted(CHANGE, {"message": ""}),
    ),
    C(
        "set_both_messages",
        "set_quantities",
        ({"1": 5},),
        setup=scripted(CHANGE, {"productAmount": 2, "message": "a", "offerLimitMessage": "b"}),
    ),
    C(
        "set_only_offer_limit_message",
        "set_quantities",
        ({"1": 5},),
        setup=scripted(CHANGE, {"productAmount": 2, "message": "", "offerLimitMessage": "kun 2"}),
    ),
    C(
        "set_expired_body_logs_in_again",
        "set_quantities",
        ({"18381": 2},),
        setup=scripted(CHANGE, EXPIRED),
    ),
    C("set_remove", "set_quantities", ({"18381": 0},)),
    C("set_empty", "set_quantities", ({},)),
    C("set_sends_absolute_count_per_product", "set_quantities", ({"a": 1, "b": 0, "c": 50},)),
]

CASES = CART_CASES + SEARCH_CASES + SET_CASES
