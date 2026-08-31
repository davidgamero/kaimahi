#!/usr/bin/env python3
"""Fail-closed check on `make chat` output: require an A2A task with
status.state == "completed" and a non-empty reply. Reads the captured chat
output from the file named in argv[1] (P1 shipped this inline in CI; P2
factors it out so the keyless preset-switch e2e can reuse it)."""
import json
import re
import sys

raw = open(sys.argv[1]).read()
m = re.search(r"^\{.*\}$", raw, re.M | re.S)
if not m:
    sys.exit("no JSON task object found in chat output")
d = json.loads(m.group(0))
state = d.get("status", {}).get("state")
texts = [p.get("text", "") for a in d.get("artifacts", [])
         for p in a.get("parts", [])]
reply = "\n".join(t for t in texts if t.strip())
print(f"state={state}\nreply:\n{reply}")
sys.exit(0 if state == "completed" and reply else 1)
