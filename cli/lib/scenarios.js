import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const HERE = dirname(fileURLToPath(import.meta.url));
const SCENARIO_DIR = join(HERE, "..", "scenarios");

/**
 * Worked examples from docs/SCENARIOS.md, turned into something you can
 * actually stand up. A scenario supplies sensible defaults; every one of them
 * can be overridden by an explicit flag.
 */
const SCENARIOS = {
  billing: {
    name: "billing-investigator",
    file: "billing-dispute.md",
    description:
      "Investigates an unexpected mobile bill increase. Read-only: it explains, it cannot act.",
    // Governed by default: the scenario's whole point is delegating something
    // consequential, so it should be metered and budgeted from the first call.
    model: "governed-ollama",
    // One read tool, explicitly allowlisted, through the governed seam so the
    // call is authenticated and audited. The agent physically cannot change a
    // plan or contact a provider — the boundary the scenario asks for is
    // enforced by the absence of a tool, not by the model's manners.
    tools: ["kaimahi-tools:k8s_get_resources"],
  },
};

export function scenarioNames() {
  return Object.keys(SCENARIOS);
}

export async function loadScenario(name) {
  const scenario = SCENARIOS[name];
  if (!scenario) {
    throw new Error(
      `unknown scenario '${name}'. Known scenarios: ${scenarioNames().join(", ")}`,
    );
  }
  const instructions = await readFile(join(SCENARIO_DIR, scenario.file), "utf8");
  return { ...scenario, instructions };
}
