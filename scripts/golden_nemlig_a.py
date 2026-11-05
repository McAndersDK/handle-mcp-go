"""nemlig store cases, part 1: login, search and offers."""

from __future__ import annotations

import httpx

from scripts.golden_fakes import resp
from scripts.golden_hooks import advance, basket, body, put, respond, save_login, scripted
from scripts.golden_stores import Case
from tests.test_nemlig import fixture

C = Case
KEBAB = "https://www.nemlig.com/maaltider/opskrift/kebabfad-med-fladbrod-98001303"
SEARCH = "/searchgateway/api/search"
PAGE = "/productbff/api/web/page"
DIALOG = "/productbff/api/web/meals/recipeProductDialog"
DETAILS = "/productbff/api/web/meals/recipeDetails/kebabfad-med-fladbrod-98001303"
ALL = "/productbff/api/web/meals/allRecipes"
QUEUE = resp(302, headers={"Location": "https://nemlig.queue-it.net/?c=nemlig"})
NO_SLOT = basket(DeliveryTimeSlot=...)
ROW = {"ingredientTitle": "Løg", "item": {"product": {"id": "1", "title": "Løg"}}}
OFFER_PAGE = {
    "pageContent": [
        {
            "header": {"title": "Skarp pris"},
            "products": [
                {"id": 1, "title": "Dyrere", "price": 20000, "priceOriginal": 15895},
                {
                    "id": 2,
                    "title": "Gave",
                    "price": 0,
                    "priceOriginal": 2045,
                    "labelMaxQuantity": 1,
                },
                {
                    "id": 3,
                    "title": "Uden før-pris",
                    "price": 1000,
                    "availability": {"type": "SoldOut"},
                    "pricePerUnit": {"value": 5000, "unitType": "Weight"},
                    "description": "200 g",
                },
                {
                    "id": 4,
                    "title": "Mix",
                    "price": 1500,
                    "priceOriginal": 2000,
                    "labelMaxQuantity": 3,
                    "campaignLines": [{"text": "Mix 2 stk. 35,-"}, {"text": "x"}],
                    "pricePerUnit": {"value": 7550, "unitType": "Pieces"},
                },
                {"id": 5, "title": "Lige pris", "price": 1500, "priceOriginal": 1500},
                {
                    "id": 6,
                    "title": "Volumen",
                    "price": 999,
                    "priceOriginal": 1999,
                    "pricePerUnit": {"value": 1, "unitType": "Volume"},
                },
            ],
        },
        {"header": {"title": "  DAGSTILBUD "}, "products": None},
        {"header": {"title": "Frugt og grønt"}, "products": [{"id": 9, "title": "x", "price": 1}]},
    ]
}

SEARCH_CASES = [
    C("search_cocio", "search", ("cocio", 5)),
    C("search_default_limit", "search", ("mælk",)),
    C("search_gift", "search", ("mums rosinboller", 10)),
    C("search_kyllingebryst_10", "search", ("kyllingebryst", 10)),
    C("search_kyllingebryst_50", "search", ("kyllingebryst", 50)),
    C("search_broccoli", "search", ("broccoli", 50)),
    C("search_hvidloeg", "search", ("hvidløg", 50)),
    C("search_hakkede_tomater", "search", ("hakkede tomater", 50)),
    C("search_hakkede_tomater_30", "search", ("hakkede tomater 30", 50)),
    C("search_makrel_tomat", "search", ("makrel i tomat", 50)),
    C("search_session_login", "search", ("cocio", 5), login="session"),
    C("search_no_login", "search", ("mælk",), login=None),
    C("search_invalid_keychain", "search", ("mælk",), login="invalid"),
    C("search_wrong_password", "search", ("mælk",), login="wrong_password"),
    C("search_login_without_redirect", "search", ("mælk",), setup=body("/webapi/login", {})),
    C(
        "search_login_not_json",
        "search",
        ("mælk",),
        setup=respond("/webapi/login", resp(text="<html>")),
    ),
    C("search_login_503", "search", ("mælk",), setup=scripted("/webapi/login", 503, 503, 503)),
    C("search_pages_in_queue", "search", ("mælk",), setup=put("queue", True)),
    C(
        "search_settings_fail",
        "search",
        ("mælk",),
        setup=scripted("/webapi/basket/GetBasket", 404),
        then=[("search", ("brød",), None)],
    ),
    C("search_settings_reused", "search", ("mælk",), then=[("search", ("brød",), None)]),
    C("search_settings_expire", "search", ("mælk",), then=[("search", ("brød",), advance(601))]),
    C(
        "search_settings_not_expired",
        "search",
        ("mælk",),
        then=[("search", ("brød",), advance(599))],
    ),
    C("search_format_changed", "search", ("mælk",), setup=body(SEARCH, {"Products": None})),
    C("search_format_changed_key", "search", ("mælk",), setup=body(SEARCH, {"Products": {}})),
    C("search_empty_body", "search", ("mælk",), setup=respond(SEARCH, resp())),
    C("search_not_json", "search", ("mælk",), setup=respond(SEARCH, resp(text="<html>nej</html>"))),
    C("search_401_renewed", "search", ("mælk",), setup=scripted(SEARCH, 401)),
    C("search_401_login_again", "search", ("mælk",), setup=scripted(SEARCH, 401, 401)),
    C("search_401_refused", "search", ("mælk",), setup=scripted(SEARCH, 401, 401, 401)),
    C(
        "search_401_refused_session",
        "search",
        ("mælk",),
        login="session",
        setup=scripted(SEARCH, 401, 401, 401),
    ),
    C("search_503_twice", "search", ("mælk",), setup=scripted(SEARCH, 503, 503)),
    C("search_503_gives_up", "search", ("mælk",), setup=scripted(SEARCH, 503, 503, 503)),
    C("search_429_gives_up", "search", ("mælk",), setup=scripted(SEARCH, 429, 429, 429)),
    C("search_400", "search", ("mælk",), setup=scripted(SEARCH, 400)),
    C("search_404", "search", ("mælk",), setup=scripted(SEARCH, 404)),
    C(
        "search_read_error_then_ok",
        "search",
        ("mælk",),
        setup=lambda f, s: f.scripted.update({SEARCH: [httpx.ReadError("reset"), 503]}),
    ),
    C(
        "search_timeout_gives_up",
        "search",
        ("mælk",),
        setup=lambda f, s: f.scripted.update({SEARCH: [httpx.ReadTimeout("t") for _ in range(3)]}),
    ),
    C(
        "search_other_http_error",
        "search",
        ("mælk",),
        setup=lambda f, s: f.scripted.update({SEARCH: [httpx.UnsupportedProtocol("x")]}),
    ),
    C("search_queue_it_redirect", "search", ("mælk",), setup=respond(SEARCH, QUEUE)),
]

OFFER_CASES = [
    C("offers_default", "offers", (10,)),
    C("offers_default_limit", "offers"),
    C("offers_limit_1", "offers", (1,)),
    C("offers_session", "offers", (10,), login="session"),
    C("offers_no_login", "offers", (10,), login=None),
    C(
        "offers_anonymous_token_renewed",
        "offers",
        (5,),
        login=None,
        then=[("offers", (5,), scripted(PAGE, 401))],
    ),
    C(
        "offers_anonymous_refused",
        "offers",
        (5,),
        login=None,
        then=[("offers", (5,), scripted(PAGE, 401, 401))],
    ),
    C(
        "offers_login_saved_after_anonymous",
        "offers",
        (5,),
        login=None,
        then=[("offers", (5,), save_login("nemlig", "password"))],
    ),
    C("offers_no_timeslot", "offers", (10,), setup=NO_SLOT),
    C("offers_one_broken_list", "offers", (10,), setup=lambda f, s: _broken_skarp_pris(f)),
    C(
        "offers_all_broken",
        "offers",
        (10,),
        setup=body(
            PAGE,
            {
                "pageContent": [
                    {"header": {"title": "Skarp pris"}, "products": [{}]},
                    {"header": {"title": "Dagstilbud"}, "products": [{"id": 1}]},
                ]
            },
        ),
    ),
    C("offers_no_lists", "offers", (10,), setup=body(PAGE, {"pageContent": []})),
    C("offers_empty_body", "offers", (10,), setup=respond(PAGE, resp())),
    C("offers_parsing_edge_cases", "offers", (10,), login=None, setup=body(PAGE, OFFER_PAGE)),
    C("offers_queue_it_api", "offers", (10,), setup=put("api_queue", True)),
    C("offers_pages_in_queue", "offers", (10,), setup=put("queue", True)),
    C("offers_gift_search_fails", "offers", (10,), setup=scripted(SEARCH, 500, 500, 500)),
    C("offers_gift_search_format_changed", "offers", (10,), setup=body(SEARCH, {"Products": None})),
]


def _broken_skarp_pris(fake) -> None:
    page = fixture("offers_page")
    page["pageContent"][1]["products"][0].pop("id")
    fake.respond[PAGE] = [resp(200, page)]


CASES = SEARCH_CASES + OFFER_CASES
