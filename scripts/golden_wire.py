"""wire goldens: a real stdio subprocess of the MCP server, driven with JSON-RPC lines.

The subprocess (golden_wire_server) has the same guards as this process and clean fake stores.
Recorded per call: the request, the parsed response, the raw text of the result and the text
without Python's prefix `Error executing tool <name>: `.
"""

from __future__ import annotations

import json
import os
import queue
import subprocess
import sys
import threading
from typing import Any

from scripts.golden_io import ROOT
from tests.test_privacy import CALLS

PREFIX = "Error executing tool {}: "
PROTOCOL = "2025-06-18"
TIMEOUT = 90

ERRORS = [
    ("get_cart", {"store": "nope"}),
    ("get_cart", {"store": ""}),
    ("set_cart_quantities", {"store": "fake", "items": {"1": 99}}),
    ("set_cart_quantities", {"store": "fake", "items": {"1": -1}}),
    ("set_cart_quantities", {"store": "fake", "items": {"1": "mange"}}),
    ("set_cart_quantities", {"store": "fake", "items": {}}),
    ("set_cart_quantities", {"store": "fake", "items": {str(i): 1 for i in range(51)}}),
    ("add_recipe_to_cart", {"store": "fake", "recipe": "r1", "skip": ["Pizza"]}),
    ("add_recipe_to_cart", {"store": "fake", "recipe": "r1", "swap": {"mælk": "999"}}),
    ("add_recipe_to_cart", {"store": "fake", "recipe": "r1", "include": ["Peber"]}),
    ("add_recipe_to_cart", {"store": "fake", "recipe": "r1", "skip": ["mælk"]}),
    ("add_recipe_to_cart", {"store": "fake", "recipe": "r1", "persons": 99, "swap": {"mælk": "1"}}),
    ("search_products", {"query": "mælk", "stores": ["broken"]}),
    ("search_products", {"query": "mælk", "stores": ["nope"]}),
    ("search_products", {"query": "mælk", "stores": ["fake", "broken"], "sort": "price"}),
    ("search_products", {"query": "mælk", "sort": "billigst"}),
    ("search_products", {"query": "mælk", "stores": ["fake"], "limit": 0}),
    ("search_products", {"query": "mælk", "stores": ["fake"], "limit": 999}),
    ("search_products", {}),
    ("search_recipes", {"query": "karry", "stores": ["broken"]}),
    ("get_orders", {"store": "broken"}),
    ("get_order", {"store": "fake"}),
    ("get_recipe", {"store": "fake", "recipe": "r1", "persons": 1}),
    ("get_recipe", {"store": "fake", "recipe": "r1", "persons": 99}),
    ("get_recipe", {"store": "fake", "recipe": "r1", "persons": None}),
    ("compare_basket", {"items": [" ", ""]}),
    ("compare_basket", {"items": ["mælk"] * 51}),
    ("compare_basket", {"items": [" mælk 1 l ", "", "2 x rugbrød"], "stores": ["fake", "broken"]}),
    ("list_delivery_slots", {"stores": ["fake"], "days": 0}),
    ("list_delivery_slots", {"stores": ["fake"], "days": 99}),
    ("list_recipes", {"stores": ["fake"], "category": "frokost"}),
    ("list_recipes", {"stores": ["fake"], "tags": []}),
    ("get_delivery_info", {"store": "broken"}),
    ("get_cart", {}),
    ("list_offers", {"stores": ["broken", "fake"], "limit": 1}),
    ("no_such_tool", {}),
    ("get_cart", {"store": "fake", "extra": 1}),
]


class Wire:
    """One server subprocess, newline-delimited JSON-RPC over pipes (no sockets)."""

    def __init__(self, mode: str) -> None:
        env = {**os.environ, "PYTHONUTF8": "1", "PYTHONIOENCODING": "utf-8"}
        self.proc = subprocess.Popen(
            [sys.executable, "-m", "scripts.golden_wire_server", mode],
            cwd=ROOT,
            env=env,
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        self.lines: queue.Queue[bytes] = queue.Queue()
        self.stderr: list[bytes] = []
        threading.Thread(
            target=self._pump, args=(self.proc.stdout, self.lines.put), daemon=True
        ).start()
        threading.Thread(
            target=self._pump, args=(self.proc.stderr, self.stderr.append), daemon=True
        ).start()
        self.next_id = 0

    @staticmethod
    def _pump(stream, sink) -> None:
        for line in iter(stream.readline, b""):
            sink(line)

    def send(self, message: dict) -> None:
        self.proc.stdin.write((json.dumps(message, ensure_ascii=False) + "\n").encode("utf-8"))
        self.proc.stdin.flush()

    def call(self, method: str, params: dict | None = None) -> tuple[dict, str]:
        self.next_id += 1
        self.send({"jsonrpc": "2.0", "id": self.next_id, "method": method, "params": params or {}})
        while True:
            try:
                line = self.lines.get(timeout=TIMEOUT)
            except queue.Empty:
                raise RuntimeError(
                    f"no answer to {method}: {b''.join(self.stderr)[-2000:]!r}"
                ) from None
            raw = line.decode("utf-8").rstrip("\r\n")
            message = json.loads(raw)
            if message.get("id") == self.next_id:
                return message, raw

    def notify(self, method: str) -> None:
        self.send({"jsonrpc": "2.0", "method": method})

    def close(self) -> None:
        self.proc.stdin.close()
        try:
            self.proc.wait(timeout=30)
        except subprocess.TimeoutExpired:
            self.proc.kill()


def handshake(wire: Wire) -> dict:
    params = {
        "protocolVersion": PROTOCOL,
        "capabilities": {},
        "clientInfo": {"name": "golden", "version": "0"},
    }
    response, raw = wire.call("initialize", params)
    wire.notify("notifications/initialized")
    return {"request": params, "response": response, "response_raw": raw}


def tool_call(wire: Wire, name: str, arguments: Any) -> dict:
    params = {"name": name, "arguments": arguments}
    response, _raw = wire.call("tools/call", params)
    result = response.get("result") or {}
    text = "".join(
        c.get("text", "") for c in result.get("content") or [] if c.get("type") == "text"
    )
    prefix = PREFIX.format(name)
    stripped = text.removeprefix(prefix)
    sdk = result.get("isError") and ("validation error" in text or "Input should be" in text)
    return {
        "name": name,
        "arguments": arguments,
        "response": response,
        "is_error": bool(result.get("isError")),
        "text": text,
        "text_without_prefix": stripped,
        "text_compared": not sdk,  # Python's own argument error text: kept, not compared
    }


def session() -> dict:
    wire = Wire("clean")
    try:
        hello = handshake(wire)
        listing, raw = wire.call("tools/list")
        calls = [tool_call(wire, name, args) for name, args in CALLS]
        errors = [tool_call(wire, name, args) for name, args in ERRORS]
        ping, _ = wire.call("ping")
    finally:
        wire.close()
    return {
        "initialize": hello,
        "tools_list": {"response": listing, "response_raw": raw},
        "calls": calls,
        "errors": errors,
        "ping": ping,
    }
