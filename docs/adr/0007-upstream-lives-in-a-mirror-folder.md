# 0007: Upstream lives in a mirror folder

Python is not deleted. Upstream's whole tree (`NJohansen/handle-mcp`) lives in `upstream/`, with only location data redacted. The Go code is at the repo root (ADR 0005).

## Merging upstream

```
git fetch upstream
git merge -X subtree=upstream upstream/main
```

Use the default git config. Conflicts show only on the redacted lines; resolve them by keeping the redaction. Then:

1. Port what changed to Go by hand (ADR 0005, "Porting upstream fixes" in 0006 for pickup).
2. Re-export the goldens from the mirror: `uv run --frozen --project upstream python -m scripts.export_golden`.
3. Read the golden diff. A changed golden is new upstream behaviour: decide per case whether Go follows or gets a named exception.

## Why

- Merges are clean. The paths in upstream's commits map onto `upstream/` with the subtree option, so git sees the same files and only the redactions differ.
- Goldens can record new upstream behaviour. The export script runs the real Python code of the mirror.

## Consequences

- The fork's history contains upstream's own old commits, with upstream's real store fixtures. That is accepted, because rewriting them would break merging.
- The mirror's docs keep upstream's emoji. The no-emoji rule covers our own files, not the mirror.
- The mirror is never built, tested or edited, except for the redactions. Only `scripts/export_golden` runs its code, to make goldens.
- Nothing personal or location specific may be added to the mirror. A redaction that a merge brings back is redacted again in the merge commit.
