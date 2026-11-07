"""One-time export of what the Python code answers, as golden files for the Go port.

    uv run --frozen --project upstream python -m scripts.export_golden

The Python code and its tests are the upstream/ mirror; its pyproject.toml and uv.lock give the
environment. Safety guards run first, before handle_mcp is imported (golden_guard).
See tests/golden/README.md.
"""

from __future__ import annotations

from scripts import golden_guard

LOOP = golden_guard.install()

import json
import logging
import subprocess
import sys
from importlib.metadata import version

from scripts import golden_io as io

PYTHON = ("upstream/src", "upstream/tests", "upstream/pyproject.toml", "upstream/uv.lock")


def git(*args: str) -> str:
    done = subprocess.run(["git", *args], cwd=io.ROOT, capture_output=True, text=True, check=True)
    return done.stdout.strip()


def python_commit() -> str:
    """The last commit that changed the Python code. HEAD can be later: Go and CI commits."""
    return git("log", "-1", "--format=%H", "--", *PYTHON)


def manifest(counts: dict) -> dict:
    libs = ["mcp", "httpx", "anyio", "pydantic", "keyring", "tzdata", "pytest", "ruff"]
    skip = ("manifest.json",)
    return {
        "made": "2026-11-05",
        "command": "uv run --frozen --project upstream python -m scripts.export_golden",
        "git_head_at_export": git("rev-parse", "HEAD"),
        "python_commit": python_commit(),
        "python_files_changed_since_commit": bool(git("diff", "HEAD", "--stat", "--", *PYTHON)),
        "python": sys.version.split()[0],
        "versions": {name: version(name) for name in libs},
        "pinned_clock": "2026-11-02T12:00:00+01:00",
        "wire_protocol": "2025-06-18",
        "normalised_strings": dict(sorted(io.NORMALISED.items())),
        "counts": counts,
        "fixtures": io.tree(io.ROOT / "tests" / "fixtures"),
        "inputs": {
            "tests/live_list.json": io.sha256(io.ROOT / "tests" / "live_list.json"),
            "tests/snapshots/tools.json": io.sha256(io.ROOT / "tests" / "snapshots" / "tools.json"),
        },
        "python_sources": io.tree(io.ROOT / "upstream" / "src"),
        "golden_files": io.tree(io.GOLDEN, skip),
    }


def main() -> None:
    from scripts import (
        golden_bilka,
        golden_bilka_b,
        golden_compare,
        golden_match,
        golden_nemlig_a,
        golden_nemlig_b,
        golden_nemlig_c,
        golden_nemlig_r,
        golden_privacy,
        golden_recipecart,
        golden_wire,
        golden_wire_args,
    )
    from scripts.golden_fakes import ScenarioBilka, ScenarioNemlig
    from scripts.golden_readme import README
    from scripts.golden_stores import Pinned, export_store

    golden_guard.memory_keyring()
    golden_guard.assert_safe()
    from handle_mcp.stores.bilkatogo import BilkaStore
    from handle_mcp.stores.nemlig import NemligStore

    logging.disable(logging.INFO)  # the adapters log logins at INFO
    io.clean()
    counts: dict = {}
    golden_privacy.export()
    stats = golden_match.export()
    counts["match"] = {k: v for k, v in stats.items() if k != "sizes"}
    with Pinned():
        golden_compare.export(LOOP)
        golden_recipecart.export()
        nemlig_cases = [
            case
            for module in (golden_nemlig_a, golden_nemlig_r, golden_nemlig_b, golden_nemlig_c)
            for case in module.CASES
        ]
        stores = {
            "nemlig": (nemlig_cases, ScenarioNemlig, NemligStore),
            "bilkatogo": (golden_bilka.CASES + golden_bilka_b.CASES, ScenarioBilka, BilkaStore),
        }
        for name, (cases, fake, adapter) in stores.items():
            LOOP.run_until_complete(export_store(name, cases, fake, adapter))
            counts.setdefault("store_cases", {})[name] = len(cases)
    session = golden_wire.session()
    io.dump("wire/session.json", session)
    probes = golden_wire_args.run()
    io.dump("wire/args.json", probes)
    counts["wire"] = {
        "calls": len(session["calls"]),
        "errors": len(session["errors"]),
        "arg_probes": len(probes["probes"]),
    }
    counts["files"] = len(io.tree(io.GOLDEN))
    io.write_text(
        "README.md",
        README.format(commit=python_commit(), head=git("rev-parse", "HEAD"), protocol="2025-06-18"),
    )
    io.dump("manifest.json", manifest(counts))
    print(json.dumps({"files": counts["files"], "counts": counts}, indent=1))


if __name__ == "__main__":
    main()
