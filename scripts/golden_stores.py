"""The store golden harness: run one case against an adapter on a fake backend and record it.

A case is one or more adapter calls. For each case a new store, a new fake backend and a new
memory keychain are made, so nothing leaks between cases. The record has the calls, the answer or
the error, every HTTP request the adapter made and what it saved in the keychain.
"""

from __future__ import annotations

import json
import os
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import datetime
from types import SimpleNamespace
from typing import Any
from urllib.parse import parse_qsl

import httpx

from handle_mcp import stores as registry
from handle_mcp.privacy import jsonable
from handle_mcp.stores import _http
from handle_mcp.stores import bilkatogo as bilka_module
from handle_mcp.stores import nemlig as nemlig_module
from scripts.golden_guard import memory_keyring
from scripts.golden_io import dump

NOW = "2026-11-02T12:00:00+01:00"
HEADERS = {
    "nemlig": (
        "accept",
        "authorization",
        "content-type",
        "device-size",
        "platform",
        "referer",
        "user-agent",
        "version",
        "x-xsrf-token",
    ),
    "bilkatogo": (
        "authorization",
        "content-type",
        "jwt_token",
        "x-algolia-application-id",
        "x-algolia-api-key",
    ),
}
EMAIL = "kunde@example.com"
SECRETS = {
    "nemlig": {
        "password": {"email": EMAIL, "password": "hemmeligt"},
        "wrong_password": {"email": EMAIL, "password": "forkert"},
        "session": {"email": EMAIL, "session": {".ASPXAUTH": "auth-1"}},
        "dead_session": {"email": EMAIL, "session": {".ASPXAUTH": "old"}},
        "invalid": {"email": EMAIL},
    },
    "bilkatogo": {
        "password": {"email": EMAIL, "password": "hemmeligt"},
        "wrong_password": {"email": EMAIL, "password": "forkert"},
        "session": {"email": EMAIL, "session": {"login_token": "st2.fake-login-token"}},
        "dead_session": {"email": EMAIL, "session": {"login_token": "st2.dead"}},
        "invalid": {"email": EMAIL},
        "no_token": {"email": EMAIL, "session": {"other": "x"}},
    },
}
CLOCK = {"t": 1000.0}  # the value nemlig's time.monotonic() gives


class FixedDatetime(datetime):
    """datetime whose now() is the pinned clock; the rest is the real class."""

    @classmethod
    def now(cls, tz=None):
        return cls.fromisoformat(NOW).astimezone(tz) if tz else cls.fromisoformat(NOW)


async def _no_sleep(_seconds):
    return None


@dataclass
class Case:
    name: str
    method: str
    args: tuple = ()
    login: str | None = "password"
    setup: Callable[[Any, Any], None] | None = None
    then: list[tuple] = field(default_factory=list)  # more steps: (method, args, hook or None)
    env: dict[str, str] = field(default_factory=dict)


def request_record(store: str, r: httpx.Request) -> dict:
    ct = r.headers.get("content-type", "")
    body: Any = None
    if r.content:
        if "json" in ct:
            body = json.loads(r.content)
        elif "x-www-form-urlencoded" in ct:
            body = [list(p) for p in parse_qsl(r.content.decode())]
        else:
            body = r.content.decode("utf-8", "replace")
    cookie = r.headers.get("cookie", "")
    headers = {h: r.headers[h] for h in HEADERS[store] if h in r.headers}
    if headers.get("user-agent", "").startswith("python-httpx"):
        del headers["user-agent"]
    return {
        "method": r.method,
        "url": str(r.url.copy_with(query=None)),
        "params": [list(p) for p in r.url.params.multi_items()],
        "body": body,
        "headers": headers,
        "cookies": sorted(c.split("=")[0].strip() for c in cookie.split(";") if c.strip()),
    }


def error_record(exc: Exception) -> dict:
    kind = "StoreError" if isinstance(exc, registry.StoreError) else type(exc).__name__
    return {"type": kind, "message": str(exc)}


async def run(store_name: str, case: Case, new_fake, new_store) -> dict:
    backend = memory_keyring()
    fake = new_fake()
    follow = store_name == "nemlig"
    client = httpx.AsyncClient(transport=httpx.MockTransport(fake.handle), follow_redirects=follow)
    store = new_store(client)
    secret = SECRETS[store_name].get(case.login) if case.login else None
    if secret is not None:
        backend.secrets[(registry.KEYCHAIN_SERVICE, store_name)] = json.dumps(secret)
    saved_env = {k: os.environ.get(k) for k in case.env}
    os.environ.update(case.env)
    CLOCK["t"] = 1000.0
    if case.setup:
        case.setup(fake, store)
    steps, seen = [], 0
    try:
        for method, args, hook in [(case.method, case.args, None), *case.then]:
            if hook:
                hook(fake, store)
            step: dict = {"method": method, "args": list(args)}
            try:
                call = getattr(store, method)(*args)
                step["answer"] = jsonable(await call if method != "session" else call)
            except Exception as exc:  # noqa: BLE001
                step["error"] = error_record(exc)
            step["requests"] = [request_record(store_name, r) for r in fake.requests[seen:]]
            seen = len(fake.requests)
            steps.append(step)
    finally:
        for key, old in saved_env.items():
            if old is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = old
        await client.aclose()
    secret_after = backend.secrets.get((registry.KEYCHAIN_SERVICE, store_name))
    record: dict = {
        "name": case.name,
        "store": store_name,
        "login": case.login,
        "keychain_before": secret,
        "keychain_after": None if secret_after is None else json.loads(secret_after),
        "session_after": store.session(),
        "env": case.env,
    }
    if len(steps) == 1:
        record.update(steps[0])
    else:
        record["steps"] = steps
    return record


class Pinned:
    """The pinned clock, and no sleep in retries, for the length of the export of the stores."""

    def __enter__(self):
        self.saved = (bilka_module.datetime, nemlig_module.time, _http.asyncio)
        bilka_module.datetime = FixedDatetime
        nemlig_module.time = SimpleNamespace(monotonic=lambda: CLOCK["t"])
        _http.asyncio = SimpleNamespace(sleep=_no_sleep)

    def __exit__(self, *_exc):
        bilka_module.datetime, nemlig_module.time, _http.asyncio = self.saved


async def export_store(store_name: str, cases: list[Case], new_fake, new_store) -> dict[str, int]:
    sizes, names = {}, set()
    for case in cases:
        assert case.name not in names, f"duplicate case {case.name}"
        names.add(case.name)
        record = await run(store_name, case, new_fake, new_store)
        rel = f"stores/{store_name}/{case.name}.json"
        sizes[rel] = dump(rel, record)
    return sizes
