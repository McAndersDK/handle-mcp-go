"""Safety guards for the golden export. Imported first: nothing here imports handle_mcp.

The export must never touch a real store, a real login or a real keychain. `install()` runs
before `handle_mcp` is imported, because `stores/__init__.py` reads HANDLE_CONFIG_DIR at import.
"""

from __future__ import annotations

import asyncio
import os
import socket
import sys
import tempfile


def _refuse(*_args, **_kwargs):
    raise OSError("golden export: network access is blocked")


def install() -> asyncio.AbstractEventLoop:
    """Apply the guards and return the event loop the export must run on.

    On Windows asyncio builds its self-pipe with a loopback socketpair, which needs a connect.
    That happens inside `new_event_loop()`, so the loop is made first and then every connect
    is blocked for good. No code of the export opens a socket after this point."""
    if os.environ.get("HANDLE_LIVE"):
        sys.exit("golden export: HANDLE_LIVE is set. Refusing to run next to live tests.")
    for key in [k for k in os.environ if k.startswith("HANDLE_")]:
        del os.environ[key]
    os.environ["HANDLE_CONFIG_DIR"] = tempfile.mkdtemp(prefix="handle-golden-config-")
    if "handle_mcp" in sys.modules:
        sys.exit("golden export: handle_mcp was imported before the guards.")
    loop = asyncio.new_event_loop()
    block_sockets()
    return loop


def block_sockets() -> None:
    socket.socket.connect = _refuse  # type: ignore[method-assign]
    socket.socket.connect_ex = _refuse  # type: ignore[method-assign]
    socket.create_connection = _refuse  # type: ignore[assignment]
    socket.getaddrinfo = _refuse  # type: ignore[assignment]
    socket.gethostbyname = _refuse  # type: ignore[assignment]


def memory_keyring():
    """Replace the OS keychain with MemoryKeyring and prove it. Needs handle_mcp: call after install()."""
    import keyring

    from tests.conftest import MemoryKeyring

    backend = MemoryKeyring()
    keyring.set_keyring(backend)
    assert keyring.get_keyring() is backend, "the memory keyring did not take effect"
    return backend


def assert_safe() -> None:
    """Re-check the guards after the imports."""
    from handle_mcp import stores

    config = os.environ["HANDLE_CONFIG_DIR"]
    assert str(stores.CONFIG_DIR) == config, "CONFIG_DIR was not read from the temp folder"
    assert not os.environ.get("HANDLE_LIVE")
    try:
        socket.socket().connect(("127.0.0.1", 9))
    except OSError as exc:
        assert "blocked" in str(exc)
    else:
        raise AssertionError("socket.connect is not blocked")
