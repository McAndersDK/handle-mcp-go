"""nemlig store cases, part 4: delivery and orders."""

from __future__ import annotations

from scripts.golden_fakes import nemlig_days, resp
from scripts.golden_hooks import basket, body, put, respond, scripted
from scripts.golden_nemlig_b import DAYS, DETAIL, HISTORY, HOURS, SPOT
from scripts.golden_stores import Case
from tests.test_nemlig import fixture

C = Case


DELIVERY_CASES = [
    C("delivery_info_default", "delivery_info"),
    C("delivery_info_no_chosen_time", "delivery_info", setup=basket(FormattedDeliveryTime=None)),
    C("delivery_info_spot_empty", "delivery_info", setup=respond(SPOT, resp())),
    C(
        "delivery_info_odd_times",
        "delivery_info",
        setup=body(
            SPOT,
            {
                "TimeSlot": {"Start": "0001-01-01T00:00:00", "End": "garbage"},
                "EditDeadline": "9999-12-31T23:59:59",
            },
        ),
    ),
    C(
        "delivery_info_naive_and_utc_times",
        "delivery_info",
        setup=body(
            SPOT,
            {
                "TimeSlot": {"Start": "2026-11-03T16:00:00Z", "End": "2026-11-03T19:00:00+01:00"},
                "EditDeadline": "2026-11-02T23:00:00",
            },
        ),
    ),
    C("slots_7", "delivery_slots", (7,)),
    C("slots_default", "delivery_slots"),
    C("slots_3", "delivery_slots", (3,)),
    C("slots_1", "delivery_slots", (1,)),
    C("slots_14", "delivery_slots", (14,)),
    C("slots_endless_pages", "delivery_slots", (7,), setup=put("endless_days", True)),
    C(
        "slots_second_page_fails",
        "delivery_slots",
        (7,),
        setup=respond(DAYS, resp(200, fixture("delivery_days")), resp(503), resp(503), resp(503)),
    ),
    C(
        "slots_dst_spring_2026_03_29",
        "delivery_slots",
        (1,),
        setup=body(DAYS, nemlig_days("2026-03-29", HOURS, "2026-03-28T12:00:00")),
    ),
    C(
        "slots_dst_autumn_2026_10_25",
        "delivery_slots",
        (1,),
        setup=body(DAYS, nemlig_days("2026-10-25", HOURS, "2026-10-24T12:00:00")),
    ),
    C(
        "slots_dst_day_before_spring",
        "delivery_slots",
        (1,),
        setup=body(DAYS, nemlig_days("2026-03-28", HOURS)),
    ),
    C(
        "slots_dst_day_after_autumn",
        "delivery_slots",
        (1,),
        setup=body(DAYS, nemlig_days("2026-10-26", HOURS)),
    ),
    C(
        "slots_summer_and_winter",
        "delivery_slots",
        (1,),
        setup=body(DAYS, nemlig_days("2026-07-01", [(0, 3), (14, 21)])),
    ),
    C(
        "slots_odd_entries",
        "delivery_slots",
        (3,),
        setup=body(
            DAYS,
            {
                "DayRangeHours": [
                    {
                        "DayHours": [
                            {"StartHour": None, "EndHour": 9, "Date": "2026-11-03T00:00:00"},
                            {"StartHour": 7, "EndHour": 9, "Date": "0001-01-01T00:00:00"},
                            {"StartHour": 7, "EndHour": 9, "Date": "tekst"},
                            {
                                "StartHour": 7,
                                "EndHour": 9,
                                "Date": "2026-11-03T00:00:00",
                                "Availability": 1,
                                "Type": 5,
                                "DeliveryPrice": None,
                            },
                            {
                                "StartHour": 9,
                                "EndHour": 11,
                                "Date": "2026-11-03T00:00:00",
                                "Availability": 2,
                                "Type": 1,
                                "DeliveryPrice": 0,
                                "Deadline": "2026-11-03T08:05:00",
                            },
                            {
                                "StartHour": 11,
                                "EndHour": 13,
                                "Date": "2026-11-03T00:00:00",
                                "Availability": 0,
                                "Type": 0,
                                "DeliveryPrice": 12.5,
                                "Deadline": "garbage",
                            },
                        ]
                    }
                ]
            },
        ),
    ),
    C("slots_empty", "delivery_slots", (3,), setup=body(DAYS, {})),
    C("slots_empty_body", "delivery_slots", (3,), setup=respond(DAYS, resp())),
    C("slots_503_gives_up", "delivery_slots", (3,), setup=scripted(DAYS, 503, 503, 503)),
    C("slots_session", "delivery_slots", (2,), login="session"),
]

ORDER_CASES = [
    C("orders_default", "orders", (5,)),
    C("orders_default_limit", "orders"),
    C("orders_flex", "orders", (2,), setup=put("history", "order_history_flex")),
    C("orders_empty", "orders", (5,), setup=body(HISTORY, {})),
    C("orders_null_body", "orders", (5,), setup=respond(HISTORY, resp())),
    C(
        "orders_unknown_status",
        "orders",
        (5,),
        setup=body(
            HISTORY,
            {
                "Orders": [
                    {
                        "Id": 1,
                        "Status": 9,
                        "Total": 10,
                        "OrderDate": "2026-11-03T14:21:00",
                        "DeliveryTime": {
                            "Start": "2026-11-03T14:00:00",
                            "End": "2026-11-03T21:00:00",
                        },
                    },
                    {"Status": None},
                    {"Id": 2, "Status": 2, "Total": None},
                ]
            },
        ),
    ),
    C("order_900001", "order", ("900001",)),
    C("order_flex", "order", ("1000001",), setup=put("details", "order_details_flex")),
    C("order_not_found", "order", ("900001",), setup=body(DETAIL, {})),
    C("order_404", "order", ("900001",), setup=scripted(DETAIL, 404)),
    C(
        "order_with_total",
        "order",
        ("900001",),
        setup=lambda f, s: f.respond.update(
            {
                DETAIL: [
                    resp(
                        200,
                        {
                            "Id": "900001",
                            "Total": 99.5,
                            "Status": 4,
                            "Lines": [
                                {
                                    "ProductNumber": 1,
                                    "ProductName": "A",
                                    "Quantity": 2,
                                    "Amount": 10,
                                }
                            ],
                        },
                    )
                ]
            }
        ),
    ),
    C("order_id_empty", "order", ("",)),
    C("order_id_path", "order", ("1/../basket/ClearBasket",)),
    C("order_id_query", "order", ("12?x=1",)),
    C("order_id_arabic_digits", "order", ("١٢",)),
    C("order_id_with_newline", "order", ("12\n",)),
    C("order_id_21_digits", "order", ("1" * 21,)),
    C("order_id_20_digits", "order", ("1" * 20,)),
    C("order_session", "order", ("900001",), login="session"),
]

CASES = DELIVERY_CASES + ORDER_CASES
