"""One HTTP request with the retries every store needs.

Retry only on timeout, network errors, 429 and 5xx. Safe because every write sets an
absolute quantity. Anything else is the store's own answer and goes back to the caller.
"""

from __future__ import annotations

import asyncio
from typing import Any

import httpx

from handle_mcp.stores import StoreError

RETRIES = 2  # extra attempts
RETRYABLE = (httpx.TimeoutException, httpx.NetworkError, httpx.RemoteProtocolError)


async def request(
    client: httpx.AsyncClient, method: str, url: str, store: str, **kwargs: Any
) -> httpx.Response:
    """Send the request. `store` is the name in error messages, e.g. "nemlig.com"."""
    for attempt in range(RETRIES + 1):
        try:
            resp = await client.request(method, url, **kwargs)
        except RETRYABLE as exc:
            if attempt == RETRIES:
                raise StoreError(f"{store} svarer ikke ({type(exc).__name__}).") from exc
        except httpx.HTTPError as exc:
            raise StoreError(f"Kan ikke nå {store} ({type(exc).__name__}).") from exc
        else:
            if resp.status_code != 429 and resp.status_code < 500:
                return resp
            if attempt == RETRIES:
                raise StoreError(f"{store} svarer {resp.status_code}. Prøv igen senere.")
        await asyncio.sleep(0.5 * 2**attempt)
    raise AssertionError("unreachable")
