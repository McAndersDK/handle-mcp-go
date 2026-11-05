"""Fake store backends for the store goldens: the test fakes, plus answers a case can script.

FakeNemlig and FakeBilka come from the tests. The subclasses add `respond` (path → responses to
give first, any body) and answer a few search queries from the match_* fixtures.
"""

from __future__ import annotations

import json
from urllib.parse import parse_qs

import httpx

from tests.test_bilkatogo import FakeBilka
from tests.test_bilkatogo import fixture as bilka_fixture
from tests.test_nemlig import FakeNemlig
from tests.test_nemlig import fixture as nemlig_fixture

NEMLIG_SEARCH = {
    "broccoli": "match_broccoli",
    "hvidløg": "match_hvidloeg",
    "hakkede tomater": "match_hakkede_tomater",
    "hakkede tomater 30": "match_hakkede_tomater_30",
    "makrel i tomat": "match_makrel_tomat",
}
BILKA_SEARCH = {
    "fior di latte": "match_fior_di_latte",
    "mozzarella": "match_mozzarella",
    "hvidløg": "match_hvidloeg",
    "makrel i tomat": "match_makrel_tomat",
    "hakkede tomater": "match_hakkede_tomater",
}


def resp(status: int = 200, body=None, text: str | None = None, headers: dict | None = None):
    """A response for `respond`: JSON body, or raw text."""
    if text is not None:
        return httpx.Response(status, text=text, headers=headers)
    if body is None:
        return httpx.Response(status, headers=headers)
    return httpx.Response(status, json=body, headers=headers)


class ScenarioNemlig(FakeNemlig):
    def __init__(self) -> None:
        super().__init__()
        self.respond: dict[str, list] = {}

    def handle(self, request: httpx.Request) -> httpx.Response:
        path = request.url.path
        if self.respond.get(path):
            self.requests.append(request)
            nxt = self.respond[path].pop(0)
            if isinstance(nxt, Exception):
                raise nxt
            return nxt
        if path == "/searchgateway/api/search":
            name = NEMLIG_SEARCH.get(request.url.params.get("query"))
            if name:
                self.requests.append(request)
                return httpx.Response(200, json=nemlig_fixture(name))
        return super().handle(request)


class ScenarioBilka(FakeBilka):
    def __init__(self) -> None:
        super().__init__()
        self.respond: dict[str, list] = {}

    def handle(self, request: httpx.Request) -> httpx.Response:
        path = request.url.path
        if self.respond.get(path):
            self.requests.append(request)
            nxt = self.respond[path].pop(0)
            if isinstance(nxt, Exception):
                raise nxt
            return nxt
        if path == "/1/indexes/*/queries":
            params = parse_qs(json.loads(request.content)["requests"][0]["params"])
            name = BILKA_SEARCH.get((params.get("query") or [""])[0])
            if name and "isInOffer" not in params["filters"][0]:
                self.requests.append(request)
                return httpx.Response(200, json={"results": [bilka_fixture(name)]})
        return super().handle(request)


def nemlig_days(date: str, hours: list[tuple[int, int]], deadline: str | None = None) -> dict:
    """A GetDeliveryDays answer with one day and the given (StartHour, EndHour) slots."""
    day = {
        "DayHours": [
            {
                "Id": 9000 + i,
                "StartHour": start,
                "EndHour": end,
                "DeliveryPrice": 19.0 + i,
                "Deadline": deadline or f"{date}T00:00:00",
                "Date": f"{date}T00:00:00",
                "Type": i % 2,
                "Availability": 0,
            }
            for i, (start, end) in enumerate(hours)
        ],
        "Date": f"{date}T00:00:00",
    }
    return {"DayRangeHours": [day], "NextRangeStart": None}


def bilka_days(date: str, intervals: list[tuple[str, str]]) -> dict:
    """An AvailableDeliveryDates answer with one day and the given (start, end) intervals."""
    return {
        "availableIntervals": [
            {
                "date": date,
                "intervals": [
                    {
                        "intervalStart": start,
                        "intervalEnd": end,
                        "availability_bit": 1,
                        "closed": 0,
                        "info": [{"message": f"{start}-{end}"}],
                    }
                    for start, end in intervals
                ],
            }
        ]
    }
