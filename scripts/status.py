#!/usr/bin/env python3
"""Print a user-oriented summary of the Kaimahi resources in one cluster."""

import argparse
import json
import subprocess
import sys


def kubectl(command, *args, required=True):
    proc = subprocess.run(
        [*command, *args, "-o", "json"],
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    if proc.returncode != 0:
        if required:
            print(proc.stderr.rstrip(), file=sys.stderr)
            raise SystemExit(proc.returncode)
        return []
    return json.loads(proc.stdout).get("items", [])


def condition(resource, name):
    conditions = resource.get("status", {}).get("conditions", [])
    match = next((item for item in conditions if item.get("type") == name), None)
    if match is None:
        return "-"
    return "yes" if match.get("status") == "True" else "no"


def table(headers, rows):
    widths = [len(header) for header in headers]
    for row in rows:
        for index, value in enumerate(row):
            widths[index] = max(widths[index], len(value))
    print("  " + "  ".join(value.ljust(widths[index]) for index, value in enumerate(headers)))
    for row in rows:
        print("  " + "  ".join(value.ljust(widths[index]) for index, value in enumerate(row)))


def pod_summary(pods):
    ready = 0
    restarts = 0
    rows = []
    for pod in sorted(pods, key=lambda item: item["metadata"]["name"]):
        statuses = pod.get("status", {}).get("containerStatuses", [])
        is_ready = bool(statuses) and all(item.get("ready", False) for item in statuses)
        pod_restarts = sum(item.get("restartCount", 0) for item in statuses)
        ready += int(is_ready)
        restarts += pod_restarts
        rows.append(
            [
                pod["metadata"]["name"],
                "yes" if is_ready else "no",
                pod.get("status", {}).get("phase", "Unknown"),
                str(pod_restarts),
            ]
        )
    return ready, restarts, rows


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--context", required=True)
    parser.add_argument("--target", required=True)
    args = parser.parse_args()
    command = ["kubectl", "--context", args.context]

    agents = kubectl(command, "-n", "kagent", "get", "agents")
    models = kubectl(command, "-n", "kagent", "get", "modelconfigs")
    kagent_pods = kubectl(command, "-n", "kagent", "get", "pods")
    ollama_pods = kubectl(command, "-n", "ollama", "get", "pods", required=False)
    plane_pods = kubectl(command, "-n", "kaimahi", "get", "pods", required=False)

    agent_rows = []
    for agent in sorted(agents, key=lambda item: item["metadata"]["name"]):
        declarative = agent.get("spec", {}).get("declarative", {})
        servers = []
        for tool in declarative.get("tools") or []:
            name = (tool.get("mcpServer") or {}).get("name")
            if name:
                servers.append(name)
        agent_rows.append(
            [
                agent["metadata"]["name"],
                condition(agent, "Ready"),
                condition(agent, "Accepted"),
                declarative.get("modelConfig", "-"),
                ",".join(servers) or "none",
            ]
        )

    model_rows = []
    for model in sorted(models, key=lambda item: item["metadata"]["name"]):
        spec = model.get("spec", {})
        model_rows.append(
            [
                model["metadata"]["name"],
                spec.get("provider", "-"),
                spec.get("model", "-"),
                condition(model, "Accepted"),
            ]
        )

    kagent_ready, kagent_restarts, pod_rows = pod_summary(kagent_pods)
    ollama_ready, ollama_restarts, _ = pod_summary(ollama_pods)
    plane_ready, plane_restarts, _ = pod_summary(plane_pods)
    all_agents_ready = bool(agents) and all(
        condition(agent, "Ready") == "yes" and condition(agent, "Accepted") == "yes"
        for agent in agents
    )
    all_models_accepted = bool(models) and all(condition(model, "Accepted") == "yes" for model in models)
    runtime_ready = bool(kagent_pods) and kagent_ready == len(kagent_pods)
    if args.target == "kind":
        runtime_ready = runtime_ready and bool(ollama_pods) and ollama_ready == len(ollama_pods)
    overall_ready = all_agents_ready and all_models_accepted and runtime_ready

    print("Kaimahi status")
    print(f"  target:  {args.target}")
    print(f"  context: {args.context}")
    print(
        "  result:  "
        + (f"ready ({len(agents)} agents available)" if overall_ready else "attention required")
    )

    print("\nAgents")
    table(["NAME", "READY", "ACCEPTED", "MODEL CONFIG", "TOOL SERVER"], agent_rows)
    print("  Ready = the agent can serve requests; Accepted = kagent accepted its configuration.")

    print("\nModels")
    table(["CONFIG", "PROVIDER", "MODEL", "ACCEPTED"], model_rows)

    print("\nRuntime")
    print(f"  kagent:     {kagent_ready}/{len(kagent_pods)} pods ready, {kagent_restarts} restarts")
    if ollama_pods:
        print(f"  ollama:     {ollama_ready}/{len(ollama_pods)} pods ready, {ollama_restarts} restarts")
    else:
        print("  ollama:     not installed (expected for hosted-model targets)")
    if plane_pods:
        print(f"  governance: {plane_ready}/{len(plane_pods)} pods ready, {plane_restarts} restarts")
    else:
        print("  governance: not installed (run 'make plane' when you want budgets and audit)")

    print("\nRuntime pods")
    table(["NAME", "READY", "PHASE", "RESTARTS"], pod_rows)

    print("\nNext")
    if overall_ready:
        print("  make chat")
        tool_agent = next((row[0] for row in agent_rows if row[4] != "none"), None)
        if tool_agent:
            print(f"  make chat AGENT={tool_agent} TASK='What pods are running in the ollama namespace?'")
    else:
        print(f"  kubectl --context {args.context} -n kagent get agents,pods")
        print(f"  kubectl --context {args.context} -n kagent describe agent <name>")


if __name__ == "__main__":
    main()
