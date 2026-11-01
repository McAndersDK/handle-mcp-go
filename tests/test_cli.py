"""`handle-mcp login/logout <store>`: saves in the keychain only when the login works."""

import io
import os

import pytest

from handle_mcp import cli
from handle_mcp import stores as registry
from tests.conftest import FakeStore


class CheckingStore(FakeStore):
    async def get_cart(self):
        if registry.load_login("fake")["password"] != "rigtig":
            raise registry.StoreError("Tjek email og kodeord.")
        return await super().get_cart()


@pytest.fixture
def terminal(monkeypatch):
    monkeypatch.setattr(registry, "STORES", {"fake": CheckingStore})

    def type_in(email, password):
        monkeypatch.setattr("sys.stdin", io.StringIO(email + "\n"))
        monkeypatch.setattr(cli.getpass, "getpass", lambda _prompt: password)

    return type_in


def test_login_saves_a_working_login(terminal, memory_keychain, capsys):
    terminal("a@example.com", "rigtig")
    assert cli.run(["login", "fake"]) == 0
    assert registry.keychain_get("fake") == {"email": "a@example.com", "password": "rigtig"}
    out = capsys.readouterr()
    assert out.out == ""  # nothing on stdout, and the password is never shown
    assert "rigtig" not in out.err and "Logget ind" in out.err


def test_wrong_password_saves_nothing(terminal, memory_keychain, capsys):
    terminal("a@example.com", "forkert")
    assert cli.run(["login", "fake"]) == 1
    assert registry.keychain_get("fake") is None
    assert "Tjek email og kodeord" in capsys.readouterr().err


def test_logout_removes_the_login(memory_keychain):
    registry.keychain_set("fake", "a@example.com", "x")
    assert cli.run(["logout", "fake"]) == 0
    assert registry.keychain_get("fake") is None


def test_unknown_command_or_store_shows_usage(capsys):
    assert cli.run(["login", "netto"]) == 2
    assert "Brug: handle-mcp login" in capsys.readouterr().err


def test_failed_relogin_keeps_the_old_login(terminal, memory_keychain):
    registry.keychain_set("fake", "a@example.com", "rigtig")
    terminal("a@example.com", "forkert")
    assert cli.run(["login", "fake"]) == 1
    assert registry.keychain_get("fake") == {"email": "a@example.com", "password": "rigtig"}
    assert "HANDLE_FAKE_PASSWORD" not in os.environ  # the check leaves no env vars


def test_logout_without_a_keychain_says_so(monkeypatch, capsys):
    from keyring.errors import NoKeyringError

    def no_keychain(*_args):
        raise NoKeyringError("no backend")

    monkeypatch.setattr(registry.keyring, "delete_password", no_keychain)
    assert cli.run(["logout", "fake"]) == 1
    assert "Ingen nøglering" in capsys.readouterr().err


@pytest.mark.parametrize("secret", ["not json", '{"email": "a@example.com"}', "[]"])
def test_broken_keychain_entry_gives_a_clear_error(memory_keychain, secret):
    memory_keychain.secrets[(registry.KEYCHAIN_SERVICE, "fake")] = secret
    with pytest.raises(registry.StoreError, match="Ugyldigt login i nøgleringen"):
        registry.keychain_get("fake")


def test_load_login_surfaces_a_broken_keychain_entry(memory_keychain):
    memory_keychain.secrets[(registry.KEYCHAIN_SERVICE, "fake")] = "not json"
    with pytest.raises(registry.StoreError, match="handle-mcp login fake"):
        registry.load_login("fake")
