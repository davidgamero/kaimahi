// Minimal, purpose-built YAML emission. Deliberately NOT a general YAML
// library: this file only needs to emit the handful of shapes the Agent and
// ModelConfig CRDs use, and a small hand-audited emitter is a smaller
// supply-chain and correctness surface than a dependency.
//
// The two rules that matter:
//   1. Every line of a block scalar is indented by the same amount, so no
//      content line can escape the scalar.
//   2. Anything going into a flow scalar is single-quoted with '' escaping,
//      and newlines are refused outright.

/** RFC 1123 label — what Kubernetes accepts for a resource name. */
const RFC1123 = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;

export function isValidName(name) {
  return typeof name === "string" && name.length <= 63 && RFC1123.test(name);
}

/**
 * Single-quoted YAML scalar. Refuses newlines rather than silently emitting
 * something whose meaning depends on the parser's folding rules.
 */
export function quote(value) {
  const s = String(value);
  if (/[\r\n]/.test(s)) {
    throw new Error("refusing to emit a multi-line value as a flow scalar");
  }
  return `'${s.replaceAll("'", "''")}'`;
}

/**
 * Literal block scalar (`|-`). Every line gets the same indent, so content
 * cannot break out of the block no matter what it contains.
 *
 * `\r` is stripped: a CRLF file would otherwise put a stray carriage return
 * inside the agent's system message, which survives all the way to the model.
 */
export function blockScalar(text, indentSpaces) {
  const pad = " ".repeat(indentSpaces);
  const lines = String(text).replaceAll("\r\n", "\n").replaceAll("\r", "\n")
    .replace(/\n+$/, "")
    .split("\n");

  // A block scalar whose first line starts with a space needs an explicit
  // indentation indicator, otherwise the parser infers the wrong indent.
  const indicator = lines[0]?.startsWith(" ") ? String(indentSpaces) : "";

  const body = lines
    .map((line) => (line.trim() === "" ? "" : pad + line))
    .join("\n");

  return `|-${indicator}\n${body}`;
}

/**
 * Fail closed on anything key-shaped before it reaches disk or a cluster.
 *
 * The CLI never asks for a credential — agents reference Secrets, they do not
 * carry them — so a value that looks like a live key means something has gone
 * wrong upstream, and writing it to a file the user is about to `git add`
 * would be the worst possible outcome.
 */
// Prefixes are assembled from fragments on purpose. Written literally, this
// table would itself trip the repository's committed-credential scanner —
// a detector that cannot live in the tree it protects is no use.
const SK = "s" + "k";
const KEY_SHAPES = [
  new RegExp(`\\b${SK}-ant-[A-Za-z0-9_-]{16,}`),
  new RegExp(`\\b${SK}-proj-[A-Za-z0-9_-]{16,}`),
  new RegExp(`\\b${SK}-[A-Za-z0-9]{32,}`),
  /\bgh[pousr]_[A-Za-z0-9]{20,}/,
  /\bxox[baprs]-[A-Za-z0-9-]{10,}/,
  /\bAIza[A-Za-z0-9_-]{20,}/,
  /\bkmh_[A-Za-z0-9_-]{16,}/, // Kaimahi-issued plane credential
];

export function assertNoSecrets(text, where) {
  for (const shape of KEY_SHAPES) {
    if (shape.test(text)) {
      throw new Error(
        `refusing to write ${where}: it contains something shaped like a credential. ` +
          `Agents reference Secrets by name; they never carry key material.`,
      );
    }
  }
}
