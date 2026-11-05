"""nemlig store cases, part 3: cart, quantities and terms."""

from __future__ import annotations

import httpx

from scripts.golden_fakes import resp
from scripts.golden_hooks import (
    basket,
    body,
    chain,
    forget_cookies,
    respond,
    scripted,
)
from scripts.golden_stores import Case

C = Case
CART = "/webapi/basket/GetBasket"
ADD = "/webapi/basket/AddToBasket"
DAYS = "/webapi/v2/Delivery/GetDeliveryDays"
SPOT = "/webapi/Order/DeliverySpot"
HISTORY = "/webapi/order/GetBasicOrderHistory"
DETAIL = "/webapi/v2/order/GetOrderHistory/900001"
LINE = {"Id": "9", "Name": "Æg", "Quantity": 0, "Price": 28.0}
HOURS = [(0, 2), (1, 3), (2, 3), (2, 4), (3, 5), (4, 6), (7, 9), (14, 21), (22, 24), (23, 2)]


CART_CASES = [
    C("cart_default", "get_cart"),
    C("cart_session", "get_cart", login="session"),
    C("cart_password_replaced_by_session", "get_cart", login="password"),
    C("cart_dead_session", "get_cart", login="dead_session"),
    C("cart_wrong_password", "get_cart", login="wrong_password"),
    C("cart_no_login", "get_cart", login=None),
    C("cart_invalid_keychain", "get_cart", login="invalid"),
    C(
        "cart_hides_removed_lines",
        "get_cart",
        setup=basket(
            Lines=[
                {"Id": "5064654", "Name": "Vare", "Quantity": 1, "Price": 10.0},
                LINE,
                {"Id": 7, "Quantity": None, "Price": None},
            ]
        ),
    ),
    C(
        "cart_empty",
        "get_cart",
        setup=basket(Lines=[], TotalPrice=0, TotalProductsPrice=0, NumberOfProducts=0),
    ),
    C("cart_minimal", "get_cart", setup=body(CART, {"Email": "kunde@example.com"})),
    C("cart_total_missing", "get_cart", setup=basket(TotalPrice=...)),
    C("cart_products_total_missing", "get_cart", setup=basket(TotalProductsPrice=...)),
    C(
        "cart_fees_zero_or_missing",
        "get_cart",
        setup=basket(DeliveryPrice=0, TotalBagsPrice=None, TotalDepositsPrice=3.5),
    ),
    C(
        "cart_validation_failures",
        "get_cart",
        setup=basket(ValidationFailures=[{"Message": "Mangler"}, "tekst", {"Andet": 1}, 5]),
    ),
    C("cart_minimum_met", "get_cart", setup=basket(IsMinTotalValid=True, MinimumOrderTotal=0)),
    C("cart_anonymous_basket_is_ok_with_password", "get_cart", setup=basket(Email=None)),
    C("cart_401_renewed", "get_cart", then=[("get_cart", (), scripted(CART, 401))]),
    C(
        "cart_session_refused_everywhere",
        "get_cart",
        login="session",
        then=[("get_cart", (), scripted(CART, 401, 401, 401))],
    ),
    C(
        "cart_session_cookie_dropped",
        "get_cart",
        login="session",
        then=[("get_cart", (), chain(forget_cookies))],
    ),
    C("cart_503_twice", "get_cart", setup=scripted(CART, 503, 503)),
    C("cart_503_gives_up", "get_cart", setup=scripted(CART, 503, 503, 503)),
    C(
        "cart_read_error",
        "get_cart",
        setup=lambda f, s: f.scripted.update({CART: [httpx.ReadError("reset"), 503]}),
    ),
    C("cart_400", "get_cart", setup=scripted(CART, 400)),
    C("cart_not_json", "get_cart", setup=respond(CART, resp(text="<html>"))),
    C(
        "cart_queue_it",
        "get_cart",
        setup=respond(CART, resp(302, headers={"Location": "https://nemlig.queue-it.net/"})),
    ),
    C("cart_logs_in_once", "get_cart", then=[("get_cart", (), None)]),
]

SET_CASES = [
    C("set_absolute_and_remove", "set_quantities", ({"701025": 5, "5064654": 0},)),
    C("set_sold_out", "set_quantities", ({"sold-out": 2},)),
    C("set_one_failure_continues", "set_quantities", ({"1": 1, "2": 1},), setup=scripted(ADD, 400)),
    C("set_all_fail_503", "set_quantities", ({"1": 1},), setup=scripted(ADD, 503, 503, 503)),
    C("set_empty", "set_quantities", ({},)),
    C("set_session", "set_quantities", ({"701025": 3},), login="session"),
    C(
        "set_session_cookie_dropped",
        "get_cart",
        login="session",
        then=[("set_quantities", ({"1": 2},), forget_cookies)],
    ),
    C("set_dead_session", "set_quantities", ({"1": 2},), login="dead_session"),
    C("set_response_not_json", "set_quantities", ({"1": 2},), setup=respond(ADD, resp(text="x"))),
    C("set_empty_answer", "set_quantities", ({"1": 2},), setup=respond(ADD, resp())),
    C(
        "set_two_then_cart",
        "set_quantities",
        ({"701025": 1, "5064654": 2},),
        then=[("get_cart", (), None)],
    ),
]

TERMS_CASES = [
    C("terms_default", "terms"),
    C("terms_empty_cart_estimates_bags", "terms", setup=basket(Lines=[], TotalBagsPrice=0)),
    C(
        "terms_empty_cart_bag_fee_comma",
        "terms",
        setup=basket(Lines=[]),
        env={"HANDLE_NEMLIG_BAG_FEE": " 12,50 "},
    ),
    C(
        "terms_empty_cart_bag_fee_dot",
        "terms",
        setup=basket(Lines=[]),
        env={"HANDLE_NEMLIG_BAG_FEE": "12.5"},
    ),
    C(
        "terms_empty_cart_bag_fee_bad",
        "terms",
        setup=basket(Lines=[]),
        env={"HANDLE_NEMLIG_BAG_FEE": "ikke et tal"},
    ),
    C(
        "terms_empty_cart_bag_fee_empty",
        "terms",
        setup=basket(Lines=[]),
        env={"HANDLE_NEMLIG_BAG_FEE": ""},
    ),
    C("terms_products_no_bags", "terms", setup=basket(TotalBagsPrice=0)),
    C("terms_removed_lines_only", "terms", setup=basket(Lines=[LINE], TotalBagsPrice=0)),
    C("terms_minimal", "terms", setup=body(CART, {"Email": "x"})),
    C("terms_session", "terms", login="session"),
]

CASES = CART_CASES + SET_CASES + TERMS_CASES
