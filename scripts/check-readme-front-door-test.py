#!/usr/bin/env python3
"""Exercise the README front-door checker with synthetic documents."""
import importlib.util
import subprocess
import sys
import tempfile
from pathlib import Path

CHECKER = Path(__file__).with_name("check-readme-front-door.py")
spec = importlib.util.spec_from_file_location("front_door", CHECKER)
front_door = importlib.util.module_from_spec(spec)
spec.loader.exec_module(front_door)

STABLE = (
    "curl -fsSL https://raw.githubusercontent.com/kaimahi-agents/kaimahi/main/install.sh | sh "
    '&& "$HOME/.local/bin/kmx" up '
    '&& "$HOME/.local/bin/kmx" agent chat hello-world "Who are you?"'
)
EDGE = (
    'GOBIN="$HOME/.local/bin" go install github.com/kaimahi-agents/kaimahi/cmd/kmx@main '
    '&& "$HOME/.local/bin/kmx" up && "$HOME/.local/bin/kmx" orka install '
    '&& "$HOME/.local/bin/kmx" orka status'
)
GOOD = f'''<img src="brand/hero.png">
# Kaimahi
## Get agents onto Orka
## Quickstart
```bash
{STABLE}
```
## Bleeding Edge
```bash
{EDGE}
```
## Migrate model traffic
## Status
## Documentation
'''

CASES = [("valid front door", GOOD, None)]
for label, literal in [
    ("hero image", '<img src="brand/hero.png">\n'),
    ("product line", "## Get agents onto Orka\n"),
    ("Quickstart heading", "## Quickstart\n"),
    ("Bleeding Edge heading", "## Bleeding Edge\n"),
    ("migration heading", "## Migrate model traffic\n"),
    ("Status heading", "## Status\n"),
    ("documentation heading", "## Documentation\n"),
]:
    CASES.append((f"missing {label}", GOOD.replace(literal, ""), f"{label} is missing"))

CASES += [
    ("stable install only", GOOD.replace(STABLE, STABLE.split(" && ")[0]), "stable block"),
    ("edge install only", GOOD.replace(EDGE, EDGE.split(" && ")[0]), "bleeding-edge block"),
    ("edge uses latest", GOOD.replace("cmd/kmx@main", "cmd/kmx@latest"), "bleeding-edge block"),
    ("extra stable command", GOOD.replace(STABLE + "\n", STABLE + "\nkmx version\n"), "stable block"),
    ("comment in edge block", GOOD.replace(EDGE + "\n", "# Unstable\n" + EDGE + "\n"), "bleeding-edge block"),
    ("wrong stable language", GOOD.replace("```bash", "```shell", 1), "stable command block"),
    ("third block", GOOD.replace("## Status", "```text\noutput\n```\n## Status"), "exactly two fenced"),
]

failed = 0
for name, text, expected in CASES:
    result = front_door.check(text)
    ok = (result is None) if expected is None else (result is not None and expected in result)
    print(("ok  " if ok else "FAIL") + f" [{name}] -> {result or 'valid'}")
    failed += not ok

with tempfile.TemporaryDirectory() as tmp:
    for name, text, want in [("valid", GOOD, 0), ("invalid", "# Empty README\n", 1)]:
        readme = Path(tmp) / "README.md"
        readme.write_text(text)
        got = subprocess.run([sys.executable, str(CHECKER), str(readme)], capture_output=True, text=True)
        ok = got.returncode == want and (want != 0 or "section order valid" in got.stdout)
        print(("ok  " if ok else "FAIL") + f" [as a script: {name}] -> exit {got.returncode}, want {want}")
        failed += not ok

print(f"check-readme-front-door self-test: {len(CASES) + 2} case(s), {failed} failure(s)")
sys.exit(1 if failed else 0)
