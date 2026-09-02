#!/usr/bin/env node
// kaimahi — scaffold governed agent-as-code onto Kubernetes.
//
// Noun-verb grammar (`kaimahi agent create`), so resources group as the
// surface grows: `agent create`, later `agent list`, `tool add`.
//
// Scope discipline: this tool SCAFFOLDS. It does not run agents, invoke them,
// or manage their lifecycle — kagent's CLI and kubectl already do that, and
// duplicating them is explicitly out of scope. See docs/CLI-PROPOSAL.md.

import { parseArgs } from "node:util";
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { resolve, join } from "node:path";
import { renderAgent, parseToolSpec } from "../lib/agent.js";
import { DEFAULT_PRESET, presetNames, lookupPreset, toolServerIsGoverned } from "../lib/presets.js";
import { loadScenario, scenarioNames } from "../lib/scenarios.js";
import { currentContext, isLocalContext, serverDryRun, apply, modelConfigExists } from "../lib/cluster.js";

const USAGE = `kaimahi — scaffold governed agent-as-code onto Kubernetes

Usage:
  kaimahi agent create <name> [options]
  kaimahi agent create --scenario <name> [options]

Options:
  --model <preset>        model preset (default: ${DEFAULT_PRESET})
                          ${presetNames().join(", ")}
  --instructions <file>   file containing the agent's system message
  --description <text>    one-line description
  --scenario <name>       scaffold a worked example: ${scenarioNames().join(", ")}
  --tools <server:tool[,tool]>
                          wire an MCP server; the tool allowlist is required
                          (repeatable)
  --namespace <ns>        default: kagent
  --out <dir|->           write here, or '-' for stdout (default: stdout)
  --dry-run               validate against the cluster's live CRDs
  --apply                 apply to the current context
  --context <ctx>         kube context to use
  --yes                   do not prompt
  -h, --help              this text

The tool never accepts a credential. Agents reference Secrets by name; key
material stays in the cluster, or in the governance plane.`;

function fail(message, code = 1) {
  process.stderr.write(`kaimahi: ${message}\n`);
  process.exit(code);
}

async function confirm(question) {
  if (!process.stdin.isTTY) return false;
  process.stderr.write(`${question} [y/N] `);
  const answer = await new Promise((res) => {
    process.stdin.setEncoding("utf8");
    process.stdin.once("data", (d) => res(String(d).trim().toLowerCase()));
  });
  return answer === "y" || answer === "yes";
}

async function agentCreate(argv) {
  let parsed;
  try {
    parsed = parseArgs({
      args: argv,
      allowPositionals: true,
      options: {
        model: { type: "string" },
        instructions: { type: "string" },
        description: { type: "string" },
        scenario: { type: "string" },
        tools: { type: "string", multiple: true },
        namespace: { type: "string", default: "kagent" },
        out: { type: "string" },
        "dry-run": { type: "boolean", default: false },
        apply: { type: "boolean", default: false },
        context: { type: "string" },
        yes: { type: "boolean", default: false },
        help: { type: "boolean", short: "h", default: false },
      },
    });
  } catch (err) {
    fail(err.message);
  }

  const { values: opts, positionals } = parsed;
  if (opts.help) {
    process.stdout.write(USAGE + "\n");
    return;
  }

  // A scenario supplies name, instructions, description and tools; explicit
  // flags still win, so a scenario is a starting point rather than a cage.
  let scenario = null;
  if (opts.scenario) {
    try {
      scenario = await loadScenario(opts.scenario);
    } catch (err) {
      fail(err.message);
    }
  }

  const name = positionals[0] ?? scenario?.name;
  if (!name) {
    fail("an agent name is required: kaimahi agent create <name>");
  }

  let instructions = scenario?.instructions;
  if (opts.instructions) {
    try {
      instructions = await readFile(resolve(opts.instructions), "utf8");
    } catch (err) {
      fail(`cannot read --instructions ${opts.instructions}: ${err.message}`);
    }
  }
  if (!instructions) {
    fail("no instructions: pass --instructions <file> or --scenario <name>");
  }

  const modelConfig = opts.model ?? scenario?.model ?? DEFAULT_PRESET;
  let preset;
  try {
    preset = lookupPreset(modelConfig);
  } catch (err) {
    fail(err.message);
  }

  const toolSpecs = opts.tools ?? scenario?.tools ?? [];
  let tools;
  try {
    tools = toolSpecs.map(parseToolSpec);
  } catch (err) {
    fail(err.message);
  }

  let manifest;
  try {
    manifest = renderAgent({
      name,
      namespace: opts.namespace,
      modelConfig,
      description: opts.description ?? scenario?.description,
      instructions,
      tools,
    });
  } catch (err) {
    fail(err.message);
  }

  // Write or print. Default is stdout: generate, don't mutate.
  let written = null;
  if (opts.out && opts.out !== "-") {
    const dir = resolve(opts.out);
    const path = join(dir, `${name}.yaml`);
    try {
      await mkdir(dir, { recursive: true });
      await writeFile(path, manifest, { mode: 0o644, flag: "wx" });
    } catch (err) {
      if (err.code === "EEXIST") {
        fail(`${path} already exists — refusing to overwrite a file you may have edited`);
      }
      fail(`cannot write ${path}: ${err.message}`);
    }
    written = path;
  } else if (!opts["dry-run"] && !opts.apply) {
    process.stdout.write(manifest);
  }

  const context = opts.context ?? (opts["dry-run"] || opts.apply ? await currentContext() : null);

  // Fail closed before touching the cluster: an Agent whose ModelConfig is
  // missing is admitted and then quietly fails to reconcile.
  if (opts["dry-run"] || opts.apply) {
    const present = await modelConfigExists(modelConfig, opts.namespace, context);
    if (!present) {
      fail(
        `ModelConfig '${modelConfig}' does not exist in namespace ${opts.namespace} ` +
          `on context ${context}.\n` +
          `  Create it first:  kubectl apply -f k8s/models/${modelConfig}.yaml\n` +
          `  Or for a governed preset:  make plane && make govern`,
      );
    }
  }

  if (opts["dry-run"]) {
    try {
      const out = await serverDryRun(manifest, context);
      process.stderr.write(`validated against ${context}: ${out}\n`);
    } catch (err) {
      fail(err.message);
    }
  }

  if (opts.apply) {
    // Blast radius: applying to something that is not a local dev cluster is
    // the obvious foot-gun, so it needs an explicit yes.
    if (!isLocalContext(context) && !opts.yes) {
      const ok = await confirm(
        `Context '${context}' is not a local kind/minikube cluster. Apply anyway?`,
      );
      if (!ok) fail("aborted — no changes made", 2);
    }
    try {
      const out = await apply(manifest, context);
      process.stderr.write(`${out}\n`);
    } catch (err) {
      fail(err.message);
    }
  }

  // Tell people what to do next; never pretend the job is finished.
  const notes = [];
  if (written) notes.push(`wrote ${written}`);
  if (!preset.governed) {
    notes.push(
      `WARNING: '${modelConfig}' is ungoverned — no budget, no ledger, no audit in front of it.`,
    );
  }
  for (const { server } of tools) {
    if (!toolServerIsGoverned(server)) {
      notes.push(
        `WARNING: tool server '${server}' is ungoverned — calls are not allowlisted at the ` +
          `gateway and leave no audit trail. The governed seam is 'kaimahi-tools'.`,
      );
    }
  }
  if (!preset.keyless && preset.secret) {
    notes.push(
      `this preset expects Secret '${preset.secret}' in namespace ${opts.namespace}; ` +
        `create it with make model-secret NAME=${preset.secret} (stdin only).`,
    );
  }
  if (preset.governed) {
    notes.push(
      `governed: ensure the plane is deployed (make plane) and a credential issued ` +
        `(make govern CRED=${name}), then cap it with make budget CRED=${name} CAP_CENTS=<n>.`,
    );
  }
  if (!opts.apply) {
    notes.push(
      written
        ? `apply it with kubectl apply -f ${written}`
        : `re-run with --out <dir> to save it, or --apply to send it to the cluster`,
    );
  } else {
    notes.push(`talk to it: make chat AGENT=${name}`);
  }
  for (const n of notes) process.stderr.write(`  ${n}\n`);
}

async function main() {
  const [noun, verb, ...rest] = process.argv.slice(2);

  if (!noun || noun === "-h" || noun === "--help" || noun === "help") {
    process.stdout.write(USAGE + "\n");
    return;
  }
  if (noun === "--version" || noun === "version") {
    process.stdout.write("kaimahi 0.0.0-prototype\n");
    return;
  }
  if (noun !== "agent") {
    fail(`unknown command '${noun}'. Only 'agent' exists today.\n\n${USAGE}`);
  }
  if (verb !== "create") {
    fail(
      `unknown command 'agent ${verb ?? ""}'.\n` +
        `Only 'agent create' exists. Reading, updating and deleting agents are\n` +
        `kubectl and the kagent CLI's job:\n` +
        `  kubectl -n kagent get agents\n` +
        `  kubectl -n kagent delete agent <name>\n` +
        `  kagent invoke --agent <name> --task "..."`,
    );
  }
  await agentCreate(rest);
}

main().catch((err) => fail(err?.stack || String(err)));
