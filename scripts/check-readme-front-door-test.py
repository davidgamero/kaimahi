#!/usr/bin/env python3
"""Self-test for check-readme-front-door.py against synthetic READMEs."""
import importlib.util
import sys
from pathlib import Path

spec = importlib.util.spec_from_file_location(
    "front_door", Path(__file__).with_name("check-readme-front-door.py")
)
front_door = importlib.util.module_from_spec(spec)
spec.loader.exec_module(front_door)

GOOD = """<img src="brand/hero.png">
# Kaimahi
## Governance for AI agents running on Kubernetes.
### Control model spend
### Constrain tool calls
### Approve consequential actions
<img src="docs/assets/architecture.svg">
## Quickstart
```bash
make up
make chat
```
## Status
| row |
## Proposed CLI direction
npx kaimahi create agent
"""

CASES = [
    ("valid README", GOOD, None),
    # A stray "make up" in the intro must not satisfy the quickstart marker.
    ("duplicate marker before its section", GOOD.replace("# Kaimahi\n", "# Kaimahi\nRun make up first.\n"), None),
    ("outcome after the diagram",
     GOOD.replace('### Approve consequential actions\n<img src="docs/assets/architecture.svg">',
                  '<img src="docs/assets/architecture.svg">\n### Approve consequential actions'),
     "architecture diagram is missing or out of order"),
    ("quickstart command missing", GOOD.replace("make chat\n", ""), "make chat is missing from the Quickstart command block"),
    # Prose that starts a line with the command name is not a runnable path.
    ("commands only in Quickstart prose",
     GOOD.replace("```bash\nmake up\nmake chat\n```\n", "make up the cluster, then\nmake chat with the agent.\n"),
     "Quickstart has no fenced command block"),
    ("commands in prose, a fenced block without them",
     GOOD.replace("```bash\nmake up\nmake chat\n```\n", "make up the cluster, then\nmake chat with the agent.\n```bash\nmake status\n```\n"),
     "make up is missing from the Quickstart command block"),
    ("commands only in a later section's block",
     GOOD.replace("```bash\nmake up\nmake chat\n```\n", "```bash\nmake status\n```\n").replace("| row |\n", "| row |\n```bash\nmake up\nmake chat\n```\n"),
     "make up is missing from the Quickstart command block"),
    ("heading only mentioned in prose", GOOD.replace("## Status\n", "See the Status section.\n"), "Status heading is missing"),
    ("CLI before the quickstart", GOOD.replace("## Quickstart\n", "npx kaimahi create\n## Quickstart\n"), "proposed CLI appears before"),
    ("CLI between make up and make chat", GOOD.replace("make up\nmake chat", "make up\nnpx kaimahi create\nmake chat"), "proposed CLI appears before"),
    ("CLI between make chat and Status", GOOD.replace("```\n## Status", "```\nnpx kaimahi create\n## Status"), "proposed CLI appears before"),
]

failed = 0
for name, text, expected in CASES:
    result = front_door.check(text)
    ok = (result is None) if expected is None else (result is not None and expected in result)
    print(("ok  " if ok else "FAIL") + f" [{name}] -> {result or 'valid'}")
    failed += not ok
sys.exit(1 if failed else 0)
