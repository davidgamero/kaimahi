#!/usr/bin/env python3
"""Fail when the README's public hierarchy regresses.

Every marker is anchored — a heading at the start of a line, an image
reference, a command at the start of a code-block line — and each one is
searched for only after the previous marker, so a stray mention earlier in
the file (an old paragraph that says "make up", a second heading with the
same name) cannot satisfy the check. The proposed CLI may be mentioned only
after the Status section begins: the working path and the honest status
come first.
"""
import re
import sys
from pathlib import Path

ORDER = [
    ("hero image", r'src="brand/hero\.png"'),
    ("product line", r"^## Governance for AI agents running on Kubernetes\.$"),
    ("outcome: control model spend", r"^### Control model spend$"),
    ("outcome: constrain tool calls", r"^### Constrain tool calls$"),
    ("outcome: approve consequential actions", r"^### Approve consequential actions$"),
    ("architecture diagram", r'src="docs/assets/architecture\.svg"'),
    ("Quickstart heading", r"^## Quickstart$"),
    ("Status heading", r"^## Status$"),
]
# The runnable path: both commands must be lines of the FIRST fenced code
# block in the Quickstart section, not prose that happens to name them.
QUICKSTART_COMMANDS = [("make up", r"^make up\b"), ("make chat", r"^make chat\b")]
FENCE = re.compile(r"^```[^\n]*\n(.*?)^```", re.M | re.S)
NEXT_SECTION = re.compile(r"^## ", re.M)
PROPOSED_CLI = re.compile(r"npx kaimahi create")


def quickstart_block(text: str, quickstart_end: int) -> str | None:
    """The body of the first fenced block between the Quickstart heading and the next ## heading."""
    section_end = NEXT_SECTION.search(text, quickstart_end)
    section = text[quickstart_end : section_end.start() if section_end else len(text)]
    block = FENCE.search(section)
    return block.group(1) if block else None


def check(text: str) -> str | None:
    """Return a failure message, or None when the hierarchy is valid."""
    position = 0
    status_start = None
    for label, pattern in ORDER:
        match = re.compile(pattern, re.M).search(text, position)
        if match is None:
            return f"README front door: {label} is missing or out of order"
        position = match.end()
        if label == "Quickstart heading":
            block = quickstart_block(text, position)
            if block is None:
                return "README front door: Quickstart has no fenced command block"
            command_position = 0
            for command, pattern in QUICKSTART_COMMANDS:
                found = re.compile(pattern, re.M).search(block, command_position)
                if found is None:
                    return f"README front door: {command} is missing from the Quickstart command block"
                command_position = found.end()
        if label == "Status heading":
            status_start = match.start()
    cli = PROPOSED_CLI.search(text)
    if cli is not None and cli.start() < status_start:
        return "README front door: proposed CLI appears before the Status section"
    return None


def main(path: Path) -> int:
    problem = check(path.read_text())
    if problem:
        print(problem, file=sys.stderr)
        return 1
    print("README front door: identity, outcomes, architecture, quickstart, and status order valid")
    return 0


if __name__ == "__main__":
    target = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).resolve().parents[1] / "README.md"
    raise SystemExit(main(target))
