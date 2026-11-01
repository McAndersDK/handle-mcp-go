"""Command line for a human in a terminal: save or remove a store login.

    handle-mcp login nemlig     asks for email and password, checks them, saves in the keychain
    handle-mcp logout nemlig    removes the login from the keychain

The password is never echoed, logged or written to disk in clear text.
"""

from __future__ import annotations

import asyncio
import getpass
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
        removed = registry.keychain_delete(store)
        _say("Login slettet fra nøgleringen." if removed else "Der var intet login i nøgleringen.")
        return 0

    sys.stderr.write("Email: ")
    sys.stderr.flush()
    email = sys.stdin.readline().strip()
    password = getpass.getpass("Kodeord (vises ikke): ")
    if not email or not password:
        _say("Email og kodeord skal udfyldes. Intet er gemt.")
        return 1
    try:
        registry.keychain_set(store, email, password)
    except KeyringError as exc:
        _say(f"Kunne ikke gemme i nøgleringen: {exc}")
        return 1
    try:
        asyncio.run(registry.STORES[store]().get_cart())  # a real login, read-only
    except registry.StoreError as exc:
        registry.keychain_delete(store)
        _say(f"Login virker ikke: {exc}\nIntet er gemt.")
        return 1
    _say(f"Logget ind på {store}. Login er gemt i nøgleringen.")
    return 0
