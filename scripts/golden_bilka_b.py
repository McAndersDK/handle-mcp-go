"""BilkaToGo store cases, part 2: terms, delivery slots and orders."""

from __future__ import annotations

from scripts.golden_bilka import AVAIL, DST_AUTUMN, DST_SPRING, SLOT, pickup
from scripts.golden_fakes import bilka_days
from scripts.golden_hooks import body, cart, scripted
from scripts.golden_stores import Case

C = Case


TERMS_CASES = [
    C("terms_default", "terms"),
    C("termspickup", "terms", setup=pickup),
    C("terms_no_address", "terms", setup=cart(deliveryAddress=None, stat={})),
    C("terms_session", "terms", login="session"),
    C("delivery_info_default", "delivery_info"),
    C(
        "delivery_info_slot_ended",
        "delivery_info",
        setup=cart(deliveryDate={**SLOT, "deliveryDate": "2024-09-04"}),
    ),
    C("delivery_infopickup", "delivery_info", setup=pickup),
    C("delivery_info_no_address", "delivery_info", setup=cart(deliveryAddress=None)),
    C(
        "delivery_info_no_slot_no_next",
        "delivery_info",
        setup=cart(deliveryDate=..., deliveryAddress={"dawa_uid": "x"}),
    ),
    C("slots_7", "delivery_slots", (7,)),
    C("slots_default", "delivery_slots"),
    C("slots_1", "delivery_slots", (1,)),
    C("slots_3", "delivery_slots", (3,)),
    C("slots_14", "delivery_slots", (14,)),
    C("slots_fee_unknown", "delivery_slots", setup=cart(stat={})),
    C("slots_not_home", "delivery_slots", setup=cart(deliveryAddress={"sid": "1234"})),
    C("slotspickup", "delivery_slots", setup=pickup),
    C("slots_session", "delivery_slots", (3,), login="session"),
    C(
        "slots_dst_spring_2026_03_29",
        "delivery_slots",
        (400,),
        setup=body(AVAIL, bilka_days("2026-03-29", DST_SPRING)),
    ),
    C(
        "slots_dst_autumn_2026_10_25",
        "delivery_slots",
        (400,),
        setup=body(AVAIL, bilka_days("2026-10-25", DST_AUTUMN)),
    ),
    C(
        "slots_dst_day_before_spring",
        "delivery_slots",
        (400,),
        setup=body(AVAIL, bilka_days("2026-03-28", DST_SPRING)),
    ),
    C(
        "slots_dst_day_after_autumn",
        "delivery_slots",
        (400,),
        setup=body(AVAIL, bilka_days("2026-10-26", DST_AUTUMN)),
    ),
    C(
        "slots_closed_and_bits",
        "delivery_slots",
        (400,),
        setup=body(
            AVAIL,
            {
                "availableIntervals": [
                    {
                        "date": "2026-11-03",
                        "intervals": [
                            {
                                "intervalStart": "08:00",
                                "intervalEnd": "10:00",
                                "availability_bit": 0,
                                "closed": 0,
                            },
                            {
                                "intervalStart": "10:00",
                                "intervalEnd": "12:00",
                                "availability_bit": 3,
                                "closed": 1,
                            },
                            {
                                "intervalStart": "12:00",
                                "intervalEnd": "14:00",
                                "availability_bit": 2,
                                "closed": 0,
                            },
                            {"intervalStart": "14:00", "intervalEnd": "16:00", "closed": 0},
                            {
                                "intervalStart": "16:00",
                                "intervalEnd": "18:00",
                                "availability_bit": 5,
                                "closed": 0,
                                "info": [{"message": "<b>Nat</b> a"}, {}],
                            },
                            {
                                "intervalStart": "18:00",
                                "intervalEnd": "18:00",
                                "availability_bit": 1,
                            },
                            {"intervalStart": "x", "intervalEnd": "20:00", "availability_bit": 1},
                            {
                                "intervalStart": "20:00",
                                "intervalEnd": "21:00",
                                "availability_bit": 1,
                            },
                        ],
                    },
                    {
                        "date": "garbage",
                        "intervals": [{"intervalStart": "08:00", "intervalEnd": "10:00"}],
                    },
                    {"date": "2026-11-04"},
                    {"date": "2026-11-05", "intervals": None},
                ]
            },
        ),
    ),
    C("slots_no_dates", "delivery_slots", setup=body(AVAIL, {})),
    C("slots_503_gives_up", "delivery_slots", setup=scripted(AVAIL, 503, 503, 503)),
]

ORDER_CASES = [
    C("orders_default", "orders", (5,)),
    C("orders_default_limit", "orders"),
    C("orders_empty", "orders", (5,), setup=body("/api/shop/v7/OrderHistory", {})),
    C(
        "orders_odd_entries",
        "orders",
        (5,),
        setup=body(
            "/api/shop/v7/OrderHistory",
            {
                "orders": [
                    {
                        "orderId": 5,
                        "status": "Faktureret",
                        "packedPrice": 1250,
                        "ordered": {"date": "2026-10-01"},
                    },
                    {
                        "status": "Mystisk",
                        "orderedPrice": "300",
                        "deliveryDate": "2026-10-02",
                        "deliveryInterval": {"start": "08:00:00"},
                    },
                    {
                        "status": None,
                        "price": 100,
                        "ordered": {"date": "2026-10-03", "time": "08:15:30"},
                    },
                    {},
                ]
            },
        ),
    ),
    C("order_default", "order", ("900000000002",)),
    C("order_session", "order", ("900000000002",), login="session"),
    C(
        "order_missing",
        "order",
        ("1",),
        setup=scripted("/api/shop/v7/OrderDetails", {"order": None}),
    ),
    C("order_no_order_key", "order", ("1",), setup=scripted("/api/shop/v7/OrderDetails", {})),
    C("order_id_is_sent_as_given", "order", ("12 ab?c",)),
    C(
        "order_message_body",
        "order",
        ("1",),
        setup=scripted("/api/shop/v7/OrderDetails", {"code": None, "msg": "Ingen adgang"}),
    ),
    C("recipes", "recipes", ("karry", 5)),
    C("recipe", "recipe", ("r1", 4)),
    C("featured_recipes", "featured_recipes", (20,)),
    C("recipes_by_tag", "recipes_by_tag", (["Familievenlig"], None, 20)),
]

CASES = TERMS_CASES + ORDER_CASES
