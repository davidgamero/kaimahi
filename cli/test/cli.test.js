import { test } from "node:test";
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { DEFAULT_PRESET } from "../lib/presets.js";

const run = promisify(execFile);
const BIN = join(dirname(fileURLToPath(import.meta.url)), "..", "bin", "kaimahi.js");

/**
 * End-to-end tests through the real entry point.
 *
 * The unit tests call renderAgent() with an explicit modelConfig, so they
 * cannot notice if `agent create` stops choosing a governed preset by
 * default — which is the property that matters most about this tool. These
 * exercise the argument-parsing and default-selection path instead.
 *
 * Nothing here touches a cluster: no --apply, no --dry-run.
 */
async function kaimahi(args) {
  try {
    const { stdout, stderr } = await run(process.execPath, [BIN, ...args]);
    return { code: 0, stdout, stderr };
  } catch (err) {
    return { code: err.code ?? 1, stdout: err.stdout ?? "", stderr: err.stderr ?? "" };
  }
}

test("with no --model, the CLI defaults to the governed preset", async () => {
  const { code, stdout, stderr } = await kaimahi(["agent", "create", "--scenario", "billing"]);
  assert.equal(code, 0, stderr);
  assert.match(stdout, new RegExp(`modelConfig: ${DEFAULT_PRESET}`));
  assert.match(stdout, /GOVERNED/);
  // The ungoverned warning must NOT appear on the default path.
  assert.doesNotMatch(stderr, /is ungoverned/);
});

test("the default tool seam is the governed one", async () => {
  const { stdout } = await kaimahi(["agent", "create", "--scenario", "billing"]);
  assert.match(stdout, /name: kaimahi-tools/);
  assert.match(stdout, /- 'k8s_get_resources'/);
});

test("choosing an ungoverned preset warns on stderr", async () => {
  const { code, stdout, stderr } = await kaimahi([
    "agent", "create", "--scenario", "billing", "--model", "ollama",
  ]);
  assert.equal(code, 0, stderr);
  assert.match(stdout, /modelConfig: ollama/);
  assert.match(stderr, /'ollama' is ungoverned/);
});

test("an ungoverned tool server warns even when the model is governed", async () => {
  const { stderr } = await kaimahi([
    "agent", "create", "a1",
    "--instructions", join(dirname(BIN), "..", "scenarios", "billing-dispute.md"),
    "--tools", "kagent-tool-server:k8s_get_resources",
  ]);
  assert.match(stderr, /tool server 'kagent-tool-server' is ungoverned/);
});

test("R, U and D are refused and point at the tool that does the job", async () => {
  for (const verb of ["list", "get", "delete", "update"]) {
    const { code, stderr } = await kaimahi(["agent", verb]);
    assert.notEqual(code, 0, `agent ${verb} should be refused`);
    assert.match(stderr, /kubectl -n kagent get agents/);
  }
});

test("an unknown noun is refused", async () => {
  const { code, stderr } = await kaimahi(["cluster", "create"]);
  assert.notEqual(code, 0);
  assert.match(stderr, /unknown command 'cluster'/);
});

test("a tools spec without an allowlist exits non-zero", async () => {
  const { code, stderr } = await kaimahi([
    "agent", "create", "a1",
    "--instructions", join(dirname(BIN), "..", "scenarios", "billing-dispute.md"),
    "--tools", "kaimahi-tools",
  ]);
  assert.notEqual(code, 0);
  assert.match(stderr, /no tool allowlist/);
});

test("--help works and names only the command that exists", async () => {
  const { code, stdout } = await kaimahi(["--help"]);
  assert.equal(code, 0);
  assert.match(stdout, /kaimahi agent create/);
});
