// The model presets the generator knows about, mirroring k8s/models/*.yaml.
//
// `governed` marks a preset whose baseUrl is the Kaimahi proxy rather than the
// provider: every call through it is authenticated, budget-checked, and
// ledgered, and the agent-side Secret holds a Kaimahi-issued token instead of
// a real upstream key. Governed is the default for generated agents — an
// agent scaffolded by this tool should be governed unless someone explicitly
// opts out.

export const PRESETS = {
  "governed-ollama": {
    governed: true,
    keyless: true,
    secret: "kaimahi-governed-token",
    summary: "in-cluster Ollama through the Kaimahi proxy (metered, budgeted)",
  },
  "governed-copilot": {
    governed: true,
    keyless: false,
    secret: "kaimahi-governed-token",
    summary: "Copilot models through the Kaimahi proxy (real key stays in the plane)",
  },
  ollama: {
    governed: false,
    keyless: true,
    secret: null,
    summary: "in-cluster Ollama, direct — no metering, no budget",
  },
  anthropic: { governed: false, keyless: false, secret: "anthropic-api-key", summary: "Anthropic API, direct" },
  openai: { governed: false, keyless: false, secret: "openai-api-key", summary: "OpenAI API, direct" },
  openrouter: { governed: false, keyless: false, secret: "openrouter-api-key", summary: "OpenRouter, direct" },
  "github-copilot": { governed: false, keyless: false, secret: "github-copilot-token", summary: "Copilot API, direct" },
  "azure-foundry": { governed: false, keyless: false, secret: "azure-foundry-api-key", summary: "Azure AI Foundry, direct" },
  "openai-compatible": {
    governed: false,
    keyless: false,
    secret: "openai-compatible-api-key",
    summary: "any OpenAI-compatible base URL, direct",
  },
};

export const DEFAULT_PRESET = "governed-ollama";

export function presetNames() {
  return Object.keys(PRESETS);
}

export function lookupPreset(name) {
  const preset = PRESETS[name];
  if (!preset) {
    throw new Error(
      `unknown model preset '${name}'. Known presets: ${presetNames().join(", ")}`,
    );
  }
  return preset;
}

/**
 * Tool servers, and whether calls through them are governed.
 *
 * `kaimahi-tools` is the P4b seam: the same underlying tool server, reached
 * through the enforcing MCP gateway, so every call is authenticated against a
 * kmh_ credential, checked against that credential's allowlist, and audited.
 * `kagent-tool-server` is the chart's direct front door — same tools, no
 * enforcement. Both work; only one leaves a record.
 */
export const TOOL_SERVERS = {
  "kaimahi-tools": {
    governed: true,
    summary: "kagent tool server behind the Kaimahi enforcing MCP gateway",
  },
  "kagent-tool-server": {
    governed: false,
    summary: "kagent's tool server, direct — no allowlist enforcement, no audit",
  },
};

export function toolServerIsGoverned(server) {
  // Unknown servers are reported as ungoverned rather than assumed safe.
  return TOOL_SERVERS[server]?.governed === true;
}
