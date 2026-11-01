"""`handle-mcp login/logout <store>`: saves in the keychain only when the login works."""

import io

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
