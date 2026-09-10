#!/usr/bin/env python3
"""Check the README's stable and bleeding-edge copyable setup paths."""
from __future__ import annotations

import re
import sys
from pathlib import Path

ORDER = [
    ("hero image", r'src="brand/hero\.png"'),
    ("product line", r"^## Get agents onto Orka$"),
    ("Quickstart heading", r"^## Quickstart$"),
    ("Bleeding Edge heading", r"^## Bleeding Edge$"),
    ("migration heading", r"^## Migrate model traffic$"),
    ("Status heading", r"^## Status$"),
    ("documentation heading", r"^## Documentation$"),
]
STABLE_COMMAND = (
    "curl -fsSL https://raw.githubusercontent.com/kaimahi-agents/kaimahi/main/install.sh | sh "
    '&& "$HOME/.local/bin/kmx" up '
    '&& "$HOME/.local/bin/kmx" agent chat hello-world "Who are you?"'
)
EDGE_COMMAND = (
    'GOBIN="$HOME/.local/bin" go install github.com/kaimahi-agents/kaimahi/cmd/kmx@main '
    '&& "$HOME/.local/bin/kmx" up && "$HOME/.local/bin/kmx" orka install '
    '&& "$HOME/.local/bin/kmx" orka status'
)
FENCE = re.compile(r"^```([^\n]*)\n(.*?)^```", re.M | re.S)


def check(text: str) -> str | None:
    """Return a failure message, or None when the front door is valid."""
    position = 0
    for label, pattern in ORDER:
        match = re.compile(pattern, re.M).search(text, position)
        if match is None:
            return f"README front door: {label} is missing or out of order"
        position = match.end()

    blocks = list(FENCE.finditer(text))
    if len(blocks) != 2:
        return "README front door: README must have exactly two fenced command blocks"
    for label, block, command in zip(("stable", "bleeding-edge"), blocks, (STABLE_COMMAND, EDGE_COMMAND)):
        if block.group(1).strip() != "bash":
            return f"README front door: {label} command block must be labelled bash"
        commands = [line for line in block.group(2).splitlines() if line.strip()]
        if commands != [command]:
            return f"README front door: {label} block must contain one complete copyable command"
    return None


def main(path: Path) -> int:
    problem = check(path.read_text())
    if problem:
        print(problem, file=sys.stderr)
        return 1
    print("README front door: stable and bleeding-edge commands and section order valid")
    return 0


if __name__ == "__main__":
    target = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).resolve().parents[1] / "README.md"
    raise SystemExit(main(target))
