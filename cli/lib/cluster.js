import { spawn } from "node:child_process";

/**
 * Manifests reach kubectl over stdin, never a temp file. Nothing this tool
 * generates should exist on disk unless the user asked for it with --out.
 *
 * Note for anyone tempted to simplify this to promisify(execFile): execFile
 * has no `input` option, so the child inherits an open stdin and `kubectl
 * -f -` blocks forever. The stdin write has to be explicit.
 */
function kubectl(args, { input } = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn("kubectl", args, { stdio: ["pipe", "pipe", "pipe"] });
    let stdout = "";
    let stderr = "";

    child.stdout.on("data", (d) => (stdout += d));
    child.stderr.on("data", (d) => (stderr += d));

    child.on("error", (err) => {
      reject(
        err.code === "ENOENT"
          ? new Error("kubectl not found on PATH — needed for --dry-run and --apply")
          : err,
      );
    });

    child.on("close", (code) => {
      if (code === 0) return resolve({ stdout, stderr });
      const detail = (stderr || stdout || `exit ${code}`).trim();
      reject(new Error(`kubectl ${args.join(" ")} failed:\n${detail}`));
    });

    if (input !== undefined) {
      child.stdin.on("error", () => {}); // EPIPE if kubectl bails early
      child.stdin.end(input);
    } else {
      child.stdin.end();
    }
  });
}

export async function currentContext() {
  const { stdout } = await kubectl(["config", "current-context"]);
  return stdout.trim();
}

/**
 * kind contexts are named `kind-<cluster>`. Everything else is treated as
 * potentially real infrastructure and needs an explicit yes.
 */
export function isLocalContext(context) {
  return /^kind-/.test(context) || context === "minikube" || context === "docker-desktop";
}

export async function serverDryRun(manifest, context) {
  const args = ["apply", "--dry-run=server", "-f", "-"];
  if (context) args.unshift("--context", context);
  const { stdout } = await kubectl(args, { input: manifest });
  return stdout.trim();
}

export async function apply(manifest, context) {
  const args = ["apply", "-f", "-"];
  if (context) args.unshift("--context", context);
  const { stdout } = await kubectl(args, { input: manifest });
  return stdout.trim();
}

/**
 * An Agent referencing a ModelConfig that does not exist is accepted by the
 * API server and then fails to reconcile, leaving a broken agent and a
 * message you only find by digging into status conditions. Check first and
 * say what to run instead.
 */
export async function modelConfigExists(name, namespace, context) {
  const args = ["get", "modelconfig", name, "-n", namespace, "-o", "name"];
  if (context) args.unshift("--context", context);
  try {
    await kubectl(args);
    return true;
  } catch {
    return false;
  }
}
