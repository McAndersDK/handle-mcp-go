"""Command line for a human in a terminal: save or remove a store login.

    handle-mcp login nemlig     asks for email and password, checks them, saves in the keychain
    handle-mcp logout nemlig    removes the login from the keychain

The password is never echoed, logged or written to disk in clear text.
"""

from __future__ import annotations

import asyncio
import getpass
import os
import sys

from keyring.errors import KeyringError

from handle_mcp import stores as registry


def _say(text: str) -> None:
    sys.stderr.write(text + "\n")


def run(args: list[str]) -> int:
    known = ", ".join(sorted(registry.STORES))
    if len(args) != 2 or args[0] not in ("login", "logout") or args[1] not in registry.STORES:
        _say(f"Brug: handle-mcp login <butik>  eller  handle-mcp logout <butik>\nButikker: {known}")
        return 2
    command, store = args
    if command == "logout":
        try:
            removed = registry.keychain_delete(store)
        except KeyringError as exc:
            _say(f"Ingen nøglering på denne maskine: {exc}")
            return 1
        _say("Login slettet fra nøgleringen." if removed else "Der var intet login i nøgleringen.")
        return 0

    _say(f"Log ind på {store}. Brug den email og det kodeord, du bruger på butikkens hjemmeside.")
    sys.stderr.write(f"Email til {store}: ")
    sys.stderr.flush()
    email = sys.stdin.readline().strip()
    password = getpass.getpass(f"Kodeord til {store} (vises ikke): ")
    if not email or not password:
        _say("Email og kodeord skal udfyldes. Intet er gemt.")
        return 1
    try:  # check first, save after: a failed check never touches a saved login
        _check(store, email, password)
    except registry.StoreError as exc:
        _say(f"Login blev ikke gemt: {exc}\nEt tidligere gemt login er uændret.")
        return 1
    try:
        registry.keychain_set(store, email, password)
    except KeyringError as exc:
        _say(f"Kunne ikke gemme i nøgleringen: {exc}")
        return 1
    _say(f"Logget ind på {store}. Login er gemt i nøgleringen.")
    return 0


def _check(store: str, email: str, password: str) -> None:
    """A real, read-only login with these values. They win over saved ones via env vars."""
    prefix = f"HANDLE_{store.upper()}_"
    keys = {prefix + "EMAIL": email, prefix + "PASSWORD": password}
    before = {k: os.environ.get(k) for k in keys}
    os.environ.update(keys)
    try:
        asyncio.run(registry.STORES[store]().get_cart())
    finally:
        for k, v in before.items():
            if v is None:
                os.environ.pop(k, None)
            else:
                os.environ[k] = v
