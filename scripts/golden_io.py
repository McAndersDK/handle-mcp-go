"""Writing golden files: one canonical JSON format, the three text normalisations, hashes."""

from __future__ import annotations

import dataclasses
import hashlib
import json
import re
import shutil
from collections import Counter
from datetime import date, datetime
from decimal import ROUND_HALF_UP, Context, Decimal, InvalidOperation
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
GOLDEN = ROOT / "tests" / "golden"
TOPICS = ("privacy", "match", "compare", "recipecart", "stores", "wire")

NUM = r"[0-9][0-9.eE+-]*"
# Python prints Decimal exponents and raw precision here; Go prints two decimals (ADR 0005).
TEMPLATES = {
    "minimum": re.compile(rf"(Under minimumskøb: )({NUM})( kr\. mangler til )({NUM})( kr\.)"),
    "multi_buy": re.compile(rf"([0-9]+ mere for )({NUM})( kr\.)"),
    "cheapest": re.compile(rf"(Billigst: [^()\"\\n]+ \()({NUM})( kr\. inkl\.)"),
}
NORMALISED: Counter[str] = Counter()


def two(token: str) -> str:
    try:
        return str(Decimal(token).quantize(Decimal("0.01"), rounding=ROUND_HALF_UP))
    except InvalidOperation:
        return token


def normalise(text: str) -> str:
    """Rewrite the money in exactly three message templates to two decimals. Nothing else."""
    for name, pattern in TEMPLATES.items():

        def fix(m: re.Match, name: str = name) -> str:
            parts = [two(g) if re.fullmatch(NUM, g) else g for g in m.groups()]
            out = "".join(parts)
            if out != m.group(0):
                NORMALISED[name] += 1
            return out

        text = pattern.sub(fix, text)
    return text


def plain(value: Decimal) -> str:
    """A unit-level Decimal: plain digits, no exponent, no trailing zeros ("1000", "12.95")."""
    if not value.is_finite():
        return str(value)
    if value == 0:
        return "0"
    text = format(value, "f")
    return text.rstrip("0").rstrip(".") if "." in text else text


def q10(value: Decimal) -> str:
    """per_unit: rounded half up to 10 places."""
    try:
        return plain(
            value.quantize(Decimal("1E-10"), rounding=ROUND_HALF_UP, context=Context(prec=60))
        )
    except InvalidOperation:
        return plain(value)


def raw(value: Decimal | None) -> str | None:
    """A corpus input keeps its exact str(Decimal)."""
    return None if value is None else str(value)


def ser(value: Any) -> Any:
    """Unit-level values as JSON: Decimals plain, dates isoformat, dataclasses as dicts."""
    if dataclasses.is_dataclass(value) and not isinstance(value, type):
        return {f.name: ser(getattr(value, f.name)) for f in dataclasses.fields(value)}
    if isinstance(value, Decimal):
        return plain(value)
    if isinstance(value, datetime | date):
        return value.isoformat()
    if isinstance(value, dict):
        return {str(k): ser(v) for k, v in value.items()}
    if isinstance(value, list | tuple):
        return [ser(v) for v in value]
    if isinstance(value, set | frozenset):
        return sorted(ser(v) for v in value)
    return value


def dump(rel: str, obj: Any) -> int:
    """Write tests/golden/<rel> as UTF-8, LF, sorted keys, indent 1. Returns the byte count."""
    text = json.dumps(obj, ensure_ascii=False, indent=1, sort_keys=True, allow_nan=False) + "\n"
    path = GOLDEN / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8", newline="\n") as f:
        f.write(normalise(text))
    return path.stat().st_size


def write_text(rel: str, text: str) -> None:
    path = GOLDEN / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8", newline="\n") as f:
        f.write(text)


def clean() -> None:
    """Remove what an earlier export wrote, so a renamed case never leaves a stale file."""
    for topic in TOPICS:
        shutil.rmtree(GOLDEN / topic, ignore_errors=True)
    for name in ("README.md", "manifest.json"):
        (GOLDEN / name).unlink(missing_ok=True)


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def tree(root: Path, skip: tuple[str, ...] = ()) -> dict[str, str]:
    """relative posix path → sha256, for every file under root."""
    return {
        p.relative_to(root).as_posix(): sha256(p)
        for p in sorted(root.rglob("*"))
        if p.is_file() and "__pycache__" not in p.parts and p.name not in skip
    }
