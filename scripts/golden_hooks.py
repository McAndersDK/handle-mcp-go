"""Small setup hooks for store cases. A hook is called as hook(fake, store) before a step."""

from __future__ import annotations

import json
from collections.abc import Callable

from handle_mcp.stores import KEYCHAIN_SERVICE
from scripts.golden_fakes import resp
from scripts.golden_stores import CLOCK, SECRETS

Hook = Callable[[object, object], None]


def scripted(path: str, *items) -> Hook:
    """Status codes or exceptions the fake gives first for `path`."""

    def hook(fake, _store) -> None:
        fake.scripted[path] = list(items)

    return hook


def respond(path: str, *responses) -> Hook:
    """Full responses (see fakes.resp) the fake gives first for `path`."""

    def hook(fake, _store) -> None:
        fake.respond[path] = list(responses)

    return hook


def body(path: str, data, status: int = 200) -> Hook:
    return respond(path, resp(status, data))


def put(name: str, value) -> Hook:
    def hook(fake, _store) -> None:
        setattr(fake, name, value)

    return hook


def basket(**changes) -> Hook:
    """Change the nemlig basket; a value of ... removes the key."""

    def hook(fake, _store) -> None:
        merged = {**fake.basket, **changes}
        fake.basket = {k: v for k, v in merged.items() if v is not ...}

    return hook


def cart(**changes) -> Hook:
    """Change the Bilka cart; a value of ... removes the key."""

    def hook(fake, _store) -> None:
        merged = {**fake.cart, **changes}
        fake.cart = {k: v for k, v in merged.items() if v is not ...}

    return hook


def chain(*hooks: Hook) -> Hook:
    def hook(fake, store) -> None:
        for h in hooks:
            h(fake, store)

    return hook


def forget_cookies(_fake, store) -> None:
    """The store drops our login cookie mid-run."""
    store._client.cookies.clear()


def advance(seconds: float) -> Hook:
    def hook(_fake, _store) -> None:
        CLOCK["t"] += seconds

    return hook


def save_login(store: str, kind: str) -> Hook:
    """The human logs in between two steps: a login appears in the memory keychain."""

    def hook(_fake, _store) -> None:
        import keyring

        keyring.get_keyring().secrets[(KEYCHAIN_SERVICE, store)] = json.dumps(SECRETS[store][kind])

    return hook


def expire_jwt(_fake, store) -> None:
    store._jwt = None
