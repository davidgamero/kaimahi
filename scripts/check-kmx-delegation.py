#!/usr/bin/env python3
"""Fail when a Makefile recipe re-implements what kmx owns.

D27's first condition is that there is ONE implementation of the developer
journey: kmx implements it, and the Makefile's `up`, `cluster`, `ollama`,
`model`, `kagent`, `agent`, `tools-agent`, `chat`, `status` and `down` are
thin aliases that call it. The failure this guards against is not a missing
alias — that would be obvious — but the slow kind: someone fixes a wait or
adds a flag in the Makefile because that is where they were looking, and the
two implementations drift while both stay green.

The check asks make itself rather than reading the file, so it sees the
recipe after every conditional and variable expansion — the same lines a
developer's invocation would run. `make -n` runs nothing.

The rule for an owned target on the kind path: the recipe may build kmx,
may fetch the pinned kagent CLI, and must otherwise reach the cluster ONLY
through kmx. A bare kubectl, helm or kind command in one of these recipes is
a second implementation.

The managed path (TARGET=aks) is deliberately NOT checked: milestone 1 does
not cover AKS, so those recipes are still the Makefile's own.

Run:  python3 scripts/check-kmx-delegation.py [--selftest]
"""
from __future__ import annotations

import re
import subprocess
import sys
from pathlib import Path

# Every target kmx owns, with the command it must delegate to.
OWNED = {
    "up": "kmx up",
    "cluster": "kmx up --step cluster",
    "ollama": "kmx up --step ollama",
    "model": "kmx up --step model",
    "kagent": "kmx up --step kagent",
    "agent": "kmx up --step agent",
    "tools-agent": "kmx up --step tools-agent",
    "chat": "kmx agent chat",
    "status": "kmx status",
    "down": "kmx down",
}

# A line that reaches the cluster itself. Anchored to a command position —
# the start of a line, or after a shell operator — so that `KIND_CLUSTER=x`
# in an environment prefix and `--kube-context` in an argument do not match.
CLUSTER_TOOL = re.compile(r"(?:^|[;&|(]\s*|^\s*)(kubectl|helm|kind)\s", re.M)

# There is deliberately no allow-list of "safe" lines.
#
# There was one, and it was the bug: an exemption that matched the START of a
# line (`curl …`, `go build …`) exempted the WHOLE line, so
# `curl … && kubectl apply -f extra.yaml` passed. Every legitimate line in
# these recipes — the kmx build, the pinned kagent fetch — runs no cluster
# tool at all, so the rule needs no exceptions: a line that puts kubectl, helm
# or kind in command position is a re-implementation, whatever else it does.
# The same mistake in the other direction (exempting a line because it
# mentions kmx) is covered by the self-test below.

# The kmx invocation inside a recipe line, and everything it was asked to do.
KMX_CALL = re.compile(r"\bbin/kmx\s+(?P<args>.*)$")


def kmx_invocations(recipe: str) -> list[str]:
    """The argument list of every `bin/kmx …` call in a recipe."""
    calls = []
    for line in recipe.splitlines():
        if line.lstrip().startswith("#"):
            continue
        match = KMX_CALL.search(line)
        if match:
            calls.append(match.group("args").strip())
    return calls


def offending_lines(recipe: str) -> list[str]:
    """Lines in an owned recipe that reach the cluster without kmx.

    Nothing exempts a line from this: not mentioning kmx (`$(KMX) up --step
    ollama && kubectl apply -f extra.yaml` delegates and re-implements at the
    same time), and not starting with something harmless (`curl … && kubectl
    apply -f extra.yaml`). Both are the shape that would drift, and both
    slipped past earlier versions of this check.
    """
    bad = []
    for line in recipe.splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        if CLUSTER_TOOL.search(line):
            bad.append(line.strip())
    return bad


def dry_run(target: str) -> str:
    result = subprocess.run(
        ["make", "-n", target, "TARGET=kind"],
        capture_output=True,
        text=True,
        cwd=Path(__file__).resolve().parents[1],
    )
    if result.returncode != 0:
        raise SystemExit(f"make -n {target} failed:\n{result.stdout}{result.stderr}")
    return result.stdout


def delegates(target: str, expected: str, recipe: str) -> bool:
    """Does the recipe hand kmx exactly the work this target owns?

    Exact match on the argument list, not a substring of the whole dry run:
    `make -n up` also prints its prerequisites' recipes, and "up" is a prefix
    of "up --step cluster" — so a substring search would report that `up`
    delegates even if the `up` target lost its recipe entirely and only its
    steps still called kmx.
    """
    wanted = expected.removeprefix("kmx ")
    for args in kmx_invocations(recipe):
        if args == wanted:
            return True
        # `agent chat` carries the agent and the question, which vary.
        if wanted == "agent chat" and args.startswith("agent chat "):
            return True
    return False


def check() -> int:
    problems = []
    for target, expected in OWNED.items():
        recipe = dry_run(target)
        if not delegates(target, expected, recipe):
            problems.append(f"{target}: does not delegate — expected `{expected}`")
        for line in offending_lines(recipe):
            problems.append(f"{target}: reaches the cluster without kmx: {line}")
    if problems:
        print("kmx delegation:", *problems, sep="\n  ", file=sys.stderr)
        print("\nThese targets are kmx's. Change cmd/kmx and internal/kmx, not the recipe.", file=sys.stderr)
        return 1
    print(f"kmx delegation: {len(OWNED)} targets delegate, none re-implement the journey")
    return 0


SELFTEST = [
    ("a delegating recipe", "KIND_CLUSTER='x' bin/kmx up --step agent", []),
    ("the kmx build", "go build -o bin/kmx ./cmd/kmx", []),
    ("the kagent fetch", "curl -sSfLo bin/kagent https://example/kagent-linux-amd64", []),
    ("a comment", "# kubectl apply -f k8s/ollama.yaml", []),
    ("a re-implemented apply", "kubectl --context kind-x apply -f k8s/ollama.yaml",
     ["kubectl --context kind-x apply -f k8s/ollama.yaml"]),
    ("a re-implemented helm install", "\thelm upgrade --install kagent oci://...",
     ["helm upgrade --install kagent oci://..."]),
    ("a re-implemented cluster create", "kind create cluster --name x",
     ["kind create cluster --name x"]),
    # An environment prefix is not a command: KIND_CLUSTER= must not match.
    ("an environment prefix", "KIND_CLUSTER='x' KUBE_CTX='kind-x' bin/kmx down", []),
    # A kubectl hidden behind a shell operator is still a kubectl.
    ("a chained kubectl", "true; kubectl -n kagent get pods", ["true; kubectl -n kagent get pods"]),
    # …including one chained after a genuine delegation. Mentioning kmx must
    # not exempt the rest of the line.
    ("a kubectl chained after kmx", "bin/kmx up --step ollama && kubectl apply -f k8s/extra.yaml",
     ["bin/kmx up --step ollama && kubectl apply -f k8s/extra.yaml"]),
    # The kagent-fetch exemption is anchored to the release URL, so a line
    # that merely mentions kagent-dev is still checked.
    ("a kubectl on a file named after kagent-dev", "kubectl apply -f kagent-dev-values.yaml",
     ["kubectl apply -f kagent-dev-values.yaml"]),
    # …and one chained after a line that starts harmlessly. A leading `curl`
    # or `go build` used to exempt the whole line.
    ("a kubectl chained after curl", "curl -sSfLo bin/kagent https://example/x && kubectl apply -f extra.yaml",
     ["curl -sSfLo bin/kagent https://example/x && kubectl apply -f extra.yaml"]),
    ("a helm chained after go build", "go build -o bin/kmx ./cmd/kmx; helm upgrade --install x oci://y",
     ["go build -o bin/kmx ./cmd/kmx; helm upgrade --install x oci://y"]),
]

# `up` is a prefix of `up --step cluster`, so a substring search would say the
# `up` target delegates even when its recipe is gone and only its steps call
# kmx. These pin the exact-match rule.
DELEGATION_SELFTEST = [
    ("exact match", "up", "kmx up", "KIND_CLUSTER='x' bin/kmx up", True),
    ("only the steps delegate, `up` has no recipe of its own", "up", "kmx up",
     "bin/kmx up --step cluster\nbin/kmx up --step ollama", False),
    ("no recipe at all", "up", "kmx up", "", False),
    ("a step", "agent", "kmx up --step agent", "KIND_CLUSTER='x' bin/kmx up --step agent", True),
    ("chat carries the agent and the question", "chat", "kmx agent chat",
     'bin/kmx agent chat hello-world "Who are you?"', True),
    ("chat with no arguments is not the chat recipe", "chat", "kmx agent chat", "bin/kmx agent", False),
]


def selftest() -> int:
    failed = 0
    for name, line, expected in SELFTEST:
        got = offending_lines(line)
        if got != expected:
            failed += 1
            print(f"FAIL [{name}] -> {got}, want {expected}")
        else:
            print(f"ok   [{name}]")
    for name, target, expected_command, recipe, want in DELEGATION_SELFTEST:
        got = delegates(target, expected_command, recipe)
        if got != want:
            failed += 1
            print(f"FAIL [{name}] -> delegates={got}, want {want}")
        else:
            print(f"ok   [{name}]")
    if failed:
        print(f"kmx delegation self-test: {failed} case(s) failed", file=sys.stderr)
        return 1
    print("kmx delegation self-test: all cases passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(selftest() if "--selftest" in sys.argv else check())
