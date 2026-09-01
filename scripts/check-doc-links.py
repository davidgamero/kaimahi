#!/usr/bin/env python3
"""Refuse broken relative links in the repo's Markdown.

Every `[text](target)` whose target is not an absolute URL must resolve to
a file in the tree, and a `#fragment` on a Markdown target must match a
heading in that file under GitHub's slug rules (lowercase, punctuation
dropped, spaces to hyphens, duplicates suffixed -1, -2, ...).

The docs were restructured by capability once; the old phase-named files
are stubs that forward. This is what keeps every forward, every README
pointer, and every FAQ anchor honest from now on.

Run:  python3 scripts/check-doc-links.py [file.md ...]
      (no arguments: every tracked *.md, plus untracked ones git would add)
"""
import os
import re
import subprocess
import sys

LINK = re.compile(r"(?<!\\)\[[^\]]*\]\(([^)\s]+)(?:\s+\"[^\"]*\")?\)")
HEADING = re.compile(r"^(#{1,6})\s+(.*?)\s*#*\s*$")
FENCE = re.compile(r"^\s*(```|~~~)")


def slugs(path):
    """Heading anchors as GitHub renders them."""
    seen = {}
    out = set()
    in_fence = False
    with open(path, encoding="utf-8") as f:
        for line in f:
            if FENCE.match(line):
                in_fence = not in_fence
                continue
            if in_fence:
                continue
            m = HEADING.match(line)
            if not m:
                continue
            text = m.group(2)
            text = re.sub(r"`([^`]*)`", r"\1", text)          # code spans
            text = re.sub(r"\[([^\]]*)\]\([^)]*\)", r"\1", text)  # links
            text = re.sub(r"[*_]", "", text)                  # emphasis
            slug = text.strip().lower()
            slug = re.sub(r"[^\w\- ]", "", slug)
            slug = re.sub(r" ", "-", slug)
            n = seen.get(slug, 0)
            seen[slug] = n + 1
            out.add(slug if n == 0 else f"{slug}-{n}")
    return out


def tracked_markdown():
    ls = subprocess.run(
        ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"],
        check=True, capture_output=True,
    ).stdout.decode()
    return [p for p in ls.split("\0") if p.endswith(".md")]


def check(path, slug_cache):
    problems = []
    in_fence = False
    with open(path, encoding="utf-8") as f:
        for lineno, line in enumerate(f, 1):
            if FENCE.match(line):
                in_fence = not in_fence
                continue
            if in_fence:
                continue
            for target in LINK.findall(line):
                if re.match(r"^[a-z][a-z0-9+.-]*:", target) or target.startswith("//"):
                    continue  # absolute URL, mailto:, etc.
                if target.startswith("<") and target.endswith(">"):
                    target = target[1:-1]
                file_part, _, frag = target.partition("#")
                if file_part:
                    resolved = os.path.normpath(os.path.join(os.path.dirname(path), file_part))
                else:
                    resolved = path
                if not os.path.exists(resolved):
                    problems.append(f"{path}:{lineno}: missing target {target}")
                    continue
                if frag and resolved.endswith(".md"):
                    if resolved not in slug_cache:
                        slug_cache[resolved] = slugs(resolved)
                    if frag.lower() not in slug_cache[resolved]:
                        problems.append(f"{path}:{lineno}: no heading for #{frag} in {resolved}")
    return problems


def main(argv):
    files = argv[1:] or tracked_markdown()
    if not files:
        print("check-doc-links: no Markdown files to scan — refusing to report clean.", file=sys.stderr)
        return 1
    cache = {}
    problems = []
    for p in files:
        problems.extend(check(p, cache))
    for line in problems:
        print(line)
    if problems:
        print(f"check-doc-links: {len(problems)} broken link(s) in {len(files)} file(s)", file=sys.stderr)
        return 1
    print(f"check-doc-links: {len(files)} file(s), all relative links resolve")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
