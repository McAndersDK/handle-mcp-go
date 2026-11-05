"""wire/args.json: which argument values the Python server accepts, and what the tool receives.

Each probe calls a tool of the real server (probe store) with one value for one argument and
records accepted or refused, plus the value the store saw. Python's own argument error text is
kept but marked text_compared: false (ADR 0005: Go's argument errors are Danish).
"""

from __future__ import annotations

import json

from scripts.golden_wire import Wire, handshake, tool_call

INTS = [
    2,
    2.0,
    "2",
    "2.0",
    " 2 ",
    2.5,
    "mange",
    True,
    False,
    None,
    [],
    {},
    "",
    "+2",
    "-2",
    "02",
    "2e0",
    "1e1",
    "0x2",
    "2_0",
    "٢",
    "２",
    " 2\n",
    "2 ",
    "1_000",
    "2.",
    ".5",
    "2.0.0",
    "NaN",
    "Infinity",
    10.0,
    1e30,
    -1,
    0,
    1,
    12,
    13,
    25,
    26,
    50,
    51,
    99,
    100,
    2**31,
    2**63,
    "9" * 30,
    "true",
    [2],
    {"a": 2},
    "2,0",
]
LISTS = [
    ["fake"],
    '["fake"]',
    ' ["fake"] ',
    '["fake","broken"]',
    "fake",
    '"fake"',
    "[]",
    "null",
    "{}",
    "",
    '["fake"',
    "[1]",
    '[["fake"]]',
    "true",
    "5",
    "fake,broken",
    '["fake",]',
    "['fake']",
    5,
    True,
    {},
    None,
    [],
    [1],
    ["fake", 1],
    '{"fake": 1}',
    '\t["fake"]\n',
    '["fake"] x',
]
DICTS = [
    {"1": 2},
    '{"1": 2}',
    '{"1": "2"}',
    '{"1": 2.0}',
    '{"1": true}',
    '{"1": null}',
    "{}",
    "[]",
    '[["1", 2]]',
    "null",
    "",
    "1",
    5,
    [],
    None,
    {"1": "2"},
    {"1": 2.0},
    {"1": True},
    {"1": None},
    {"1": 2.5},
    {"1": "x"},
    '{"1": 2',
    {"1": [2]},
]
STRINGS = [5, 5.5, True, None, [], {}, ["a"], "", " a ", 0]


def seen_orders(text):
    return json.loads(text)["orders"][0]["id"]


def seen_persons(text):
    return json.loads(text)["name"]


def seen_days(text):
    return json.loads(text)["fake"]["slots"][0]["note"]


def seen_limit(text):
    return json.loads(text)["fake"][0]["name"]


def seen_qty(text):
    return json.loads(text)["results"][0]["message"]


def seen_stores(text):
    return sorted(json.loads(text))


def seen_items(text):
    data = json.loads(text)["stores"]["fake"]
    return [ln["item"] for ln in data["lines"]] + data["missing"]


def seen_skipped(text):
    return [s["ingredient"] for s in json.loads(text)["skipped"]]


def seen_query(text):
    return json.loads(text)["fake"][0]["name"]


def seen_store(text):
    return json.loads(text)["store"]


def seen_id(text):
    return json.loads(text)["id"]


PROBES = [  # (tool, argument, other arguments, kind, what the tool received)
    ("get_orders", "limit", {"store": "fake"}, INTS, seen_orders),
    ("get_recipe", "persons", {"store": "fake", "recipe": "r1"}, INTS, seen_persons),
    ("list_delivery_slots", "days", {"stores": ["fake"]}, INTS, seen_days),
    ("search_products", "limit", {"query": "x", "stores": ["fake"]}, INTS, seen_limit),
    ("set_cart_quantities", "items", {"store": "fake"}, [{"1": v} for v in INTS], seen_qty),
    ("list_offers", "stores", {}, LISTS, seen_stores),
    ("search_products", "stores", {"query": "x"}, LISTS, seen_stores),
    ("compare_basket", "items", {"stores": ["fake"]}, LISTS, seen_items),
    ("add_recipe_to_cart", "skip", {"store": "fake", "recipe": "r1"}, LISTS, seen_skipped),
    ("add_recipe_to_cart", "include", {"store": "fake", "recipe": "r1"}, LISTS, seen_skipped),
    ("set_cart_quantities", "items", {"store": "fake"}, DICTS, seen_qty),
    ("add_recipe_to_cart", "swap", {"store": "fake", "recipe": "r1"}, DICTS, seen_skipped),
    ("search_products", "query", {"stores": ["fake"]}, STRINGS, seen_query),
    ("get_cart", "store", {}, STRINGS, seen_store),
    ("get_order", "order_id", {"store": "fake"}, STRINGS, seen_id),
]


def probe(wire: Wire, tool: str, arg: str, base: dict, value, seen) -> dict:
    result = tool_call(wire, tool, {**base, arg: value})
    entry = {
        "tool": tool,
        "arg": arg,
        "input": value,
        "accepted": not result["is_error"],
        "text_compared": result["text_compared"],
    }
    if result["is_error"]:
        entry["error"] = result["text_without_prefix"]
        return entry
    try:
        entry["received"] = seen(result["text"])
    except (ValueError, KeyError, IndexError, TypeError) as exc:
        entry["received"] = None
        entry["received_error"] = type(exc).__name__
        entry["answer"] = result["text"]
    return entry


def run() -> dict:
    wire = Wire("probe")
    try:
        handshake(wire)
        entries = [
            probe(wire, tool, arg, base, value, seen)
            for tool, arg, base, values, seen in PROBES
            for value in values
        ]
    finally:
        wire.close()
    return {"probes": entries}
