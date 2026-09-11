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
# Every structural marker has an independent removal case. This ensures a
# future edit cannot delete a checker rule while leaving its self-test green.
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

# The previous checker validated four separate lines with regexes, so it needed
# individual missing, order, suffix, and mid-prose cases. The new contract is
# stricter: each block must equal one complete shell line. Mutate every step
# below to prove that exact comparison, while retaining the valuable placement,
# heading-order, language, extra-content, and CLI-exit tests from the old suite.
CASES += [
    ("stable install only", GOOD.replace(STABLE, STABLE.split(" && ")[0]), "stable block"),
    ("stable missing up", GOOD.replace(' && "$HOME/.local/bin/kmx" up', ""), "stable block"),
    ("stable missing chat", GOOD.replace(' && "$HOME/.local/bin/kmx" agent chat hello-world "Who are you?"', ""), "stable block"),
    ("stable steps shuffled", GOOD.replace(
        '"$HOME/.local/bin/kmx" up && "$HOME/.local/bin/kmx" agent chat hello-world "Who are you?"',
        '"$HOME/.local/bin/kmx" agent chat hello-world "Who are you?" && "$HOME/.local/bin/kmx" up'),
     "stable block"),
    ("edge install only", GOOD.replace(EDGE, EDGE.split(" && ")[0]), "bleeding-edge block"),
    ("edge missing up", GOOD.replace(EDGE, EDGE.replace(' && "$HOME/.local/bin/kmx" up', "")), "bleeding-edge block"),
    ("edge missing Orka install", GOOD.replace(' && "$HOME/.local/bin/kmx" orka install', ""), "bleeding-edge block"),
    ("edge missing Orka status", GOOD.replace(' && "$HOME/.local/bin/kmx" orka status', ""), "bleeding-edge block"),
    ("edge Orka steps shuffled", GOOD.replace(
        '"$HOME/.local/bin/kmx" orka install && "$HOME/.local/bin/kmx" orka status',
        '"$HOME/.local/bin/kmx" orka status && "$HOME/.local/bin/kmx" orka install'),
     "bleeding-edge block"),
    ("edge uses latest", GOOD.replace("cmd/kmx@main", "cmd/kmx@latest"), "bleeding-edge block"),
    ("edge uses release", GOOD.replace("cmd/kmx@main", "cmd/kmx@v0.1.0"), "bleeding-edge block"),
    ("edge missing revision", GOOD.replace("cmd/kmx@main", "cmd/kmx@"), "bleeding-edge block"),
    ("extra stable command", GOOD.replace(STABLE + "\n", STABLE + "\nkmx version\n"), "stable block"),
    ("prompt marker in stable block", GOOD.replace(STABLE, "$ " + STABLE), "stable block"),
    ("comment in edge block", GOOD.replace(EDGE + "\n", "# Unstable\n" + EDGE + "\n"), "bleeding-edge block"),
    ("wrong stable language", GOOD.replace("```bash", "```shell", 1), "stable command block"),
    ("wrong edge language", GOOD.replace("```bash\n" + EDGE, "```shell\n" + EDGE), "bleeding-edge command block"),
    ("empty stable block", GOOD.replace(STABLE + "\n", ""), "stable block"),
    ("commands only in prose", GOOD.replace("```bash\n" + STABLE + "\n```", STABLE), "exactly two fenced"),
    ("blocks reversed", GOOD.replace(
        "```bash\n" + STABLE + "\n```\n## Bleeding Edge\n```bash\n" + EDGE + "\n```",
        "```bash\n" + EDGE + "\n```\n## Bleeding Edge\n```bash\n" + STABLE + "\n```"),
     "stable block"),
    ("third block", GOOD.replace("## Status", "```text\noutput\n```\n## Status"), "exactly two fenced"),
    ("headings out of order", GOOD.replace(
        "## Migrate model traffic\n## Status\n## Documentation",
        "## Status\n## Migrate model traffic\n## Documentation"),
     "Status heading is missing"),
    ("heading only in prose", GOOD.replace("## Status", "See Status below."), "Status heading is missing"),
    ("incidental early heading", GOOD.replace("# Kaimahi", "## Status\n# Kaimahi"), None),
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
