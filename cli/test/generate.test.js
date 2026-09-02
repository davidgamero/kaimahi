import { test } from "node:test";
import assert from "node:assert/strict";
import { blockScalar, quote, isValidName, assertNoSecrets } from "../lib/yaml.js";
import { renderAgent, parseToolSpec } from "../lib/agent.js";

test("block scalar indents every line so content cannot escape", () => {
  const evil = "line one\nspec:\n  hijacked: true\n";
  const out = blockScalar(evil, 6);
  for (const line of out.split("\n").slice(1)) {
    if (line !== "") assert.ok(line.startsWith("      "), `un-indented: ${JSON.stringify(line)}`);
  }
});

test("block scalar emits an indentation indicator when the first line is indented", () => {
  assert.match(blockScalar("  indented first line\nsecond", 6), /^\|-6\n/);
  assert.match(blockScalar("normal first line", 6), /^\|-\n/);
});

test("block scalar strips CR so CRLF files do not poison the system message", () => {
  assert.ok(!blockScalar("a\r\nb\r\n", 6).includes("\r"));
});

test("quote escapes single quotes and refuses newlines", () => {
  assert.equal(quote("it's fine"), "'it''s fine'");
  assert.throws(() => quote("a\nb"), /multi-line/);
});

test("name validation follows RFC 1123", () => {
  assert.ok(isValidName("billing-investigator"));
  assert.ok(!isValidName("Billing"));
  assert.ok(!isValidName("-leading"));
  assert.ok(!isValidName("trailing-"));
  assert.ok(!isValidName("under_score"));
  assert.ok(!isValidName("a".repeat(64)));
});

test("secret-shaped content is refused before it can be written", () => {
  // Fragments again: a literal key prefix in a test file trips the repo's
  // own committed-credential scanner.
  const sk = "s" + "k";
  assert.throws(() => assertNoSecrets(`key: ${sk}-ant-` + "a".repeat(24), "x"), /credential/);
  assert.throws(() => assertNoSecrets("token: kmh_" + "b".repeat(20), "x"), /credential/);
  assert.throws(() => assertNoSecrets("t: ghp_" + "c".repeat(24), "x"), /credential/);
  assert.doesNotThrow(() => assertNoSecrets("apiKeySecret: anthropic-api-key", "x"));
});

test("tool specs require an explicit allowlist", () => {
  assert.deepEqual(parseToolSpec("srv:a,b"), { server: "srv", tools: ["a", "b"] });
  assert.throws(() => parseToolSpec("kagent-tool-server"), /no tool allowlist/);
  assert.throws(() => parseToolSpec("kagent-tool-server:"), /no tool allowlist/);
  assert.throws(() => parseToolSpec("Bad_Server:tool"), /valid server name/);
});

// Named for what it actually covers. The DEFAULT-selection path cannot be
// tested here — renderAgent is handed a preset — so it lives in cli.test.js,
// which spawns the binary with no --model.
test("renderAgent labels governance according to the preset it is given", () => {
  const governed = renderAgent({
    name: "a1",
    modelConfig: "governed-ollama",
    instructions: "do the thing",
  });
  assert.match(governed, /modelConfig: governed-ollama/);
  assert.match(governed, /GOVERNED/);

  const direct = renderAgent({ name: "a1", modelConfig: "ollama", instructions: "x" });
  assert.match(direct, /UNGOVERNED/);
});

test("renderAgent rejects bad names, empty instructions and unknown presets", () => {
  assert.throws(() => renderAgent({ name: "Bad", modelConfig: "ollama", instructions: "x" }), /RFC 1123/);
  assert.throws(() => renderAgent({ name: "ok", modelConfig: "ollama", instructions: "  " }), /needs instructions/);
  assert.throws(() => renderAgent({ name: "ok", modelConfig: "nope", instructions: "x" }), /unknown model preset/);
});

test("renderAgent emits the tools block with the allowlist", () => {
  const y = renderAgent({
    name: "a1",
    modelConfig: "governed-ollama",
    instructions: "x",
    tools: [{ server: "kagent-tool-server", tools: ["k8s_get_resources"] }],
  });
  assert.match(y, /toolNames:\n\s+- 'k8s_get_resources'/);
});

test("a hostile description is refused, not sanitised", () => {
  // Refusing beats escaping: if a description contains a newline, something
  // unexpected is happening and the safe move is to stop rather than guess.
  assert.throws(
    () =>
      renderAgent({
        name: "a1",
        modelConfig: "ollama",
        instructions: "x",
        description: "innocent' \nspec:\n  evil: true",
      }),
    /multi-line/,
  );
});

test("hostile instructions stay inside the block scalar", () => {
  const y = renderAgent({
    name: "a1",
    modelConfig: "ollama",
    instructions: "hello\nspec:\n  evil: true\n",
  });
  // Present as literal text, indented into the scalar — never as a sibling key.
  assert.ok(y.includes("      spec:"));
  assert.ok(!/^spec:\n  evil/m.test(y));
});

test("a tool name cannot break out of the YAML list (CWE-74)", () => {
  // Reported on PR #16: tool values were trimmed but not validated, so a
  // newline could close the sequence and append an entry nobody reviewed.
  assert.throws(
    () => parseToolSpec("srv:ok\n            - secret_tool"),
    /not a valid tool name/,
  );
  assert.throws(() => parseToolSpec("srv:ok,bad name"), /not a valid tool name/);
  assert.throws(() => parseToolSpec("srv:ok,'quoted'"), /not a valid tool name/);
  assert.throws(() => parseToolSpec("srv:a:b"), /not a valid tool name/);
  assert.deepEqual(parseToolSpec("srv:k8s_get_resources,helm.list-1"), {
    server: "srv",
    tools: ["k8s_get_resources", "helm.list-1"],
  });
});

test("emitted tool names are quoted, so validation is not the only defence", () => {
  const y = renderAgent({
    name: "a1",
    modelConfig: "ollama",
    instructions: "x",
    tools: [{ server: "srv", tools: ["k8s_get_resources"] }],
  });
  assert.ok(y.includes("- 'k8s_get_resources'"));
});
