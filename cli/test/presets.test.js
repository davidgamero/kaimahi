import { test } from "node:test";
import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { dirname, join, basename } from "node:path";
import { PRESETS, DEFAULT_PRESET } from "../lib/presets.js";

const REPO = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const MODELS_DIR = join(REPO, "k8s", "models");

/**
 * The presets table in lib/presets.js duplicates facts that really live in
 * k8s/models/*.yaml — which preset names exist, and which Secret each one
 * expects. Duplication drifts: a preset added or renamed there would leave
 * the CLI generating a modelConfig reference that does not resolve, and the
 * failure would surface as an Agent that is Accepted and then never Ready.
 *
 * These tests read the manifests, so the drift is caught here instead.
 */

async function loadManifests() {
  const files = (await readdir(MODELS_DIR)).filter((f) => f.endsWith(".yaml"));
  const out = [];
  for (const f of files) {
    const text = await readFile(join(MODELS_DIR, f), "utf8");
    // These manifests are single-document, flat, and committed in this repo.
    // A regex read keeps the CLI's zero-dependency promise; the shape is
    // asserted below so a structural change fails loudly rather than
    // silently matching nothing.
    const name = text.match(/^\s{2}name:\s*(\S+)/m)?.[1];
    const secret = text.match(/^\s{2}apiKeySecret:\s*(\S+)/m)?.[1] ?? null;
    out.push({ file: f, preset: basename(f, ".yaml"), name, secret });
  }
  return out;
}

test("every committed model preset is known to the CLI", async () => {
  const manifests = await loadManifests();
  assert.ok(manifests.length >= 5, "did not find the committed model presets");
  for (const m of manifests) {
    assert.ok(
      PRESETS[m.preset],
      `k8s/models/${m.file} exists but '${m.preset}' is missing from lib/presets.js`,
    );
  }
});

test("every CLI preset has a committed manifest", async () => {
  const manifests = await loadManifests();
  const known = new Set(manifests.map((m) => m.preset));
  for (const preset of Object.keys(PRESETS)) {
    assert.ok(
      known.has(preset),
      `lib/presets.js offers '${preset}' but k8s/models/${preset}.yaml does not exist`,
    );
  }
});

test("preset filename matches the ModelConfig name it declares", async () => {
  // `--model <preset>` becomes `modelConfig: <preset>` in the Agent, which
  // resolves by metadata.name — so the two must agree.
  for (const m of await loadManifests()) {
    assert.equal(
      m.name,
      m.preset,
      `k8s/models/${m.file} declares metadata.name '${m.name}'`,
    );
  }
});

test("the Secret each preset expects matches the manifest", async () => {
  for (const m of await loadManifests()) {
    const entry = PRESETS[m.preset];
    if (!entry) continue;
    assert.equal(
      entry.secret,
      m.secret,
      `preset '${m.preset}': CLI says Secret '${entry.secret}', manifest says '${m.secret}'`,
    );
  }
});

test("secretSource is consistent with the Secret and with governance", async () => {
  for (const m of await loadManifests()) {
    const entry = PRESETS[m.preset];
    if (!entry) continue;
    if (entry.secret === null) {
      assert.equal(entry.secretSource, null, `'${m.preset}' has no Secret but names a source`);
    } else {
      assert.ok(entry.secretSource, `'${m.preset}' names a Secret but no source for it`);
    }
    // A governed preset's Secret holds a Kaimahi-issued token that
    // `make govern` mints — never a real upstream key from stdin.
    if (entry.governed) {
      assert.equal(
        entry.secretSource,
        "govern",
        `governed preset '${m.preset}' must get its Secret from make govern`,
      );
    }
  }
});

test("the default preset exists and is governed", async () => {
  const known = new Set((await loadManifests()).map((m) => m.preset));
  assert.ok(known.has(DEFAULT_PRESET), `default '${DEFAULT_PRESET}' has no manifest`);
  assert.equal(PRESETS[DEFAULT_PRESET].governed, true, "the default must be governed");
});
