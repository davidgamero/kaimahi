#!/usr/bin/env python3
"""A tiny MCP (streamable-HTTP) echo server over https — CI's SYNTHETIC
hosted upstream (P10, docs/hosted-upstreams.md).

It stands in for a real internet MCP server so the hardened dialer, the
opt-in egress allowance and the gateway's audit rows can be proven on a
kind cluster with no GitHub token anywhere (D14). It is deliberately
minimal: no sessions, no SSE, no resources — just the four methods the
gateway relays, answered as plain JSON.

  POST /mcp        initialize / notifications/initialized / tools/list /
                   tools/call (tools: `echo`, `echo_write` and
                   `pay_invoice`; all three echo their arguments back.
                   The second exists only so a NOT-allowlisted tool has a
                   name; the third only so a tool with DECLARED
                   policy-relevant fields and a money-shaped argument
                   exists for the P12 standing-constraint checks)
  POST /redirect   302 → /mcp, so the gateway's refusal of a redirecting
                   upstream can be exercised
  anything else    404

Usage: mcp-echo-server.py --cert server.crt --key server.key [--bind 0.0.0.0] [--port 443]

The certificate and key are generated at run time by
scripts/ci/synthetic-upstream.sh (a throwaway CA that lives for one job);
nothing here is committed key material.
"""
import argparse
import http.server
import json
import ssl
import sys


class Handler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):  # one line per request on stderr
        sys.stderr.write("mcp-echo: %s %s\n" % (self.command, fmt % args))

    def _send(self, status, body=b"", headers=()):
        self.send_response(status)
        for k, v in headers:
            self.send_header(k, v)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        if body:
            self.wfile.write(body)

    def _rpc(self, msg_id, result=None, error=None):
        out = {"jsonrpc": "2.0", "id": msg_id}
        if error is not None:
            out["error"] = error
        else:
            out["result"] = result
        self._send(200, json.dumps(out).encode(), [("Content-Type", "application/json")])

    def do_POST(self):
        if self.path == "/redirect":
            self._send(302, b"", [("Location", "/mcp")])
            return
        if self.path != "/mcp":
            self._send(404, b"not found\n")
            return
        n = int(self.headers.get("Content-Length") or 0)
        try:
            msg = json.loads(self.rfile.read(n) or b"{}")
        except ValueError:
            self._send(400, b"not json\n")
            return
        method = msg.get("method")
        msg_id = msg.get("id")
        if method == "initialize":
            self._rpc(msg_id, {"protocolVersion": "2025-03-26", "capabilities": {"tools": {}},
                               "serverInfo": {"name": "kaimahi-ci-mcp-echo", "version": "0"}})
        elif method == "notifications/initialized" or msg_id is None:
            self._send(202)
        elif method == "tools/list":
            self._rpc(msg_id, {"tools": [
                {"name": "echo", "description": "Echo the arguments back (read-only stand-in).",
                 "inputSchema": {"type": "object", "properties": {"text": {"type": "string"}}}},
                {"name": "echo_write", "description": "Echo the arguments back (a stand-in for a write tool).",
                 "inputSchema": {"type": "object", "properties": {"text": {"type": "string"}}}},
                {"name": "pay_invoice", "description": "Echo the arguments back (a stand-in for a consequential tool).",
                 "inputSchema": {"type": "object", "properties": {
                     "invoice_id": {"type": "string"},
                     "amount_cents": {"type": "integer"},
                     "payee_id": {"type": "string"}}}},
            ]})
        elif method == "tools/call":
            params = msg.get("params") or {}
            name = params.get("name")
            if name not in ("echo", "echo_write", "pay_invoice"):
                self._rpc(msg_id, error={"code": -32602, "message": "unknown tool %r" % name})
                return
            args = params.get("arguments") or {}
            self._rpc(msg_id, {"content": [{"type": "text", "text": "echo:" + json.dumps(args, sort_keys=True)}],
                               "isError": False})
        else:
            self._rpc(msg_id, error={"code": -32601, "message": "method not found"})


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--cert", required=True)
    ap.add_argument("--key", required=True)
    ap.add_argument("--bind", default="0.0.0.0")
    ap.add_argument("--port", type=int, default=443)
    a = ap.parse_args()
    ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    ctx.minimum_version = ssl.TLSVersion.TLSv1_2
    ctx.load_cert_chain(a.cert, a.key)
    srv = http.server.ThreadingHTTPServer((a.bind, a.port), Handler)
    srv.socket = ctx.wrap_socket(srv.socket, server_side=True)
    sys.stderr.write("mcp-echo: serving https on %s:%d\n" % (a.bind, a.port))
    srv.serve_forever()


if __name__ == "__main__":
    main()
