# Inbound: letting the outside world trigger an agent

Everything before this let an agent act on the world (chat through a
governed model, call tools through a governed gateway, post to Slack
after a human approved it). This is the other direction: an external
event, a webhook, triggers a kagent agent. It is the plane's one ingress,
so the rules are stricter than anywhere else in the repo.

The short version: the plane accepts a webhook only on a hook the
committed config names; the caller has to prove it holds that hook's
credential (a signature, preferably); a human has to have approved the
hook, bounded, before any event on it runs; the agent it triggers has to
have budget left; every decision is audited; and an event the plane
cannot record is not honoured.

## What runs where

The bridge is a fourth listener in the `kaimahi-proxy` process, port
8082, with its own Service `kaimahi-inbound` in the `kaimahi` namespace.
No new pod. It dials exactly one place: the kagent controller's A2A
endpoint, `http://kagent-controller.kagent:8083/api/a2a/<namespace>/<agent>/`.
The agent then thinks through its governed model preset, so the spend
the event causes lands in the ledger under the agent's credential, the
same way a `make chat` does.

On kind the Service is a ClusterIP and the demo reaches it through a
port-forward. Putting it on the internet (an Ingress or LoadBalancer with
TLS) is your step, not something this repo commits. Nothing here changes
that decision for you.

## Hooks

Hooks live in the committed upstream table, `k8s/plane/upstreams.yaml`,
under `inbound_hooks`. Three ship:

| hook | proof | triggers | notes |
|---|---|---|---|
| `demo` | Kaimahi signed webhook (`kaimahi-hmac`) | `hello-world` | the generic primitive; CI drives it end to end |
| `demo-bearer` | bearer token (`bearer`) | `hello-world` | for a source that can set a header but cannot sign |
| `slack-events` | Slack request signing (`slack`) | `hello-slack` | the one named source; not live-verified, see below |

Each hook names the plane credential it is bound to, how the caller
proves it, the agent it triggers, and `budget_credential`: the credential
the agent's governed preset carries (`hello-world`, issued by `make govern`,
for every agent in this repo). A hook the config does not name does not
exist. The plane answers 401 and does nothing else.

Per-hook bounds, with defaults: `max_body_bytes` (64 KiB),
`rate_per_minute` (60) and `burst` (10). Over the body limit is a 413.
Over the rate is a 429 before the plane reads anything. The rate limiter
runs before authentication on purpose: every later refusal writes an
audit row, and the bucket is what stops an unauthenticated flood from
turning into a database flood. The trade is that a flood starves its
hook rather than the plane. The limiter and the invocation queue are
in-memory, which is right for the single replica the plane runs as and
wrong for more than one.

## How a caller proves itself

Three modes. Signing is the one to use whenever the source can sign.

**`kaimahi-hmac`**, the generic signed webhook. The caller sends three
headers:

```
X-Kaimahi-Delivery:  <a unique delivery id, [A-Za-z0-9._:-]{1,128}>
X-Kaimahi-Timestamp: <unix seconds when it signed>
X-Kaimahi-Signature: v1=<hex HMAC-SHA256 over "v1:<timestamp>:<delivery>:<body>">
```

The secret is shared between the caller and the plane, and only those
two. The delivery id is inside the signed string, so a captured request
cannot be replayed under a fresh id. Timestamps more than five minutes
from the plane's clock are refused. A bad, missing, or stale signature is
one answer, 401, so a forger learns nothing from which.

**`slack`**, Slack's Events API request signing: `X-Slack-Request-Timestamp`
and `X-Slack-Signature: v0=<hex HMAC-SHA256 over "v0:<timestamp>:<body>">`,
same five-minute window. The delivery id is the envelope's `event_id`.
Slack's `url_verification` handshake is answered with the challenge
after the signature checks, audited as `challenge`, and triggers nothing.

**`bearer`**: `Authorization: Bearer kmh_...`, the hook credential's own
token, plus `X-Kaimahi-Delivery`. It is the same `kmh_` credential the
proxy and gateway use, stored by sha256 only. Weaker than a signature
because the proof travels with every request; present so a source that
cannot sign still has a path. A real credential that is not the hook's
gets 403, not 401.

The event text is the JSON body's `text` field if there is one,
otherwise the body itself (for Slack, `event.text`). It becomes the
agent's prompt as-is. On the generic hooks, a body that is not UTF-8
text, or has no text, is a 400; on the Slack hook, anything but the two
envelope shapes above is.

## Approval: a hook is granted, bounded, or it does not run

Triggering an agent from outside is consequential, so it is an
approvable action in the [approvals](approvals.md) sense, not something
a credential alone opens. An event on a hook with no live grant is
denied with 403 and files a pending request (kind `inbound`, subject the
hook name, deduped while pending). A human approves it with bounds:

```
make approvals
make approve ID=<uuid> USES=100 TTL=24h
```

Each admitted event consumes one use. When the grant expires or is
spent, the next event is denied again and a fresh request is filed. That
is the event-count lever an ingress needs most, and it comes from the
existing permit machinery unchanged: a grant with no bound is still
refused, and liveness is evaluated in SQL at admission time.

Why not gate on credential and budget alone? Because a webhook exists to
run without a human in the loop, and the honest way to give it that is a
bounded allowance a human set, not an open door. Per-event approval was
the other option and it does not fit the deny-and-retry model: a source
does not retry on our schedule, so the event would be lost between the
denial and the approval. Bounded grants keep the human's decision and let
the source deliver.

The two things an event's agent might do that need their own approval
(a tool outside its allowlist, a Slack post) are still gated where they
already were. The inbound grant admits the trigger, nothing more.

## Budget: the door checks before the agent spends

Every inbound event causes spend, so before admitting one the plane
previews the target's budget: the credential named in `budget_credential`,
its month-to-date usage, its caps, and the headroom any live budget grant
adds. If the proxy would deny the agent's next call, the door denies the
event instead, with 429, and files the agent's budget request (the same
one the proxy would file; they dedupe). If the plane cannot read spend at
all, that is a 503, not a refusal: an ingress caller has to be able to
tell "no" from "try later". The preview consumes nothing. The proxy stays
the place that consumes a budget grant use, so one event never burns two.

A target whose credential does not exist is not governed, and an
ungoverned agent cannot be triggered from outside (403). The plane cannot
verify that `budget_credential` really is the credential the agent's
preset mounts; the proxy enforces regardless, so a wrong name only makes
the door check the wrong budget, never lets spend through ungoverned.

## Replay and the audit trail

Every decision about an attributable event is a row in `inbound_audit`,
append-only like the ledger and the tool audit:

```
make inbound-audit            # every hook
make inbound-audit HOOK=demo  # one hook
```

```
created (UTC)       hook   credential     delivery        decision  status   in  out detail
2026-09-01T21:25:17 demo   inbound-demo   live-7f640710   completed    200  368    3 task 348f6222-...
2026-09-01T21:25:09 demo   inbound-demo   live-7f640710   denied       409    0    0 replay: delivery already admitted
2026-09-01T21:25:07 demo   inbound-demo   live-7f640710   admitted     202    0    0 granted eac9d995-...
2026-09-01T21:25:02 demo   inbound-demo   probe-400d34b0  denied       429    0    0 target budget: monthly token budget reached; approval request filed
2026-09-01T21:24:12 demo   inbound-demo   probe-11ac4068  denied       403    0    0 inbound trigger not permitted: no live grant for hook demo; approval request filed
2026-09-01T21:24:10 demo   inbound-demo                   denied       401    0    0 unauthorized
```

The `admitted` row is the replay guard. Its (hook, delivery id) is unique
among admitted rows, and it is inserted in the same transaction that
consumes the grant use, row first. So a replay of an admitted delivery
hits the index before any use is burned, and nothing is admitted without
a row. A delivery that was denied stays retryable on purpose: a source
retrying after "no grant yet" is delivering, not replaying.

Admitted events run asynchronously. The caller gets a 202 with the event
id, a worker sends A2A `message/send`, and when the agent answers a
second row lands: `completed` (HTTP 200, the task id, and the token counts
the agent runtime reported for that invocation) or `failed` (why). The
token counts are attribution, not billing: the proxy priced and ledgered
the same call under the agent's credential, and the two agree (that row
above and the ledger row for it both say 368 in, 3 out). An agent turn
is bounded at five minutes and the queue holds sixteen events for two
workers, so an `admitted` row can legitimately wait a while; one that
never gets an outcome means the plane restarted with it queued or in
flight. The queue is not durable, and a restart says so this way.

If the audit trail cannot be written, every event is refused with 503
until a write succeeds. The refusal's own record attempt is the recovery
probe. This is the same rule the ledger and the tool audit run under.

## Running the demo

Assumes `make up` and `make plane` are done and `make govern` has issued
`hello-world`'s credential (the door needs it).

```
make inbound-credential                  # the demo hook's identity; its bearer token is discarded
make inbound-secret HOOK=demo GENERATE=1 # a signing secret, stored plane-side
make inbound-fire                        # 403: no grant yet, request filed
make approvals
make approve ID=<uuid> USES=2 TTL=10m
make inbound-fire                        # 202: admitted
make inbound-audit                       # wait for the completed row (tens of seconds on ollama)
make inbound-fire                        # 202: second use
make inbound-fire                        # 403: spent; a fresh request filed
```

`make inbound-fire` runs `scripts/inbound-probe.sh`, which signs the
event with the key from the `kaimahi-inbound-signing` Secret and passes
only when the plane answers exactly what it expected (`EXPECT=202` by
default). Useful knobs: `EVENT='...'` for the text, `DELIVERY=<id>` to
resend the same delivery (a replay, `EXPECT=409`), `AUTH=none` for an
unauthenticated event (`EXPECT=401`), `AUTH=forged` for a wrong key.

The first `make inbound-secret` on a fresh plane can take kubelet a
minute or two to project into the pod; until then the hook answers 503
"hook signing secret unavailable". That is the fail-closed answer, not a
bug: a signed hook with no secret refuses rather than accepting unsigned.

For a caller of your own: give it the hook's key
(`kubectl -n kaimahi get secret kaimahi-inbound-signing -o jsonpath='{.data.demo}' | base64 -d`)
and have it sign as above. For a source that has its own signing secret
(Slack does), paste that secret instead: `make inbound-secret HOOK=slack-events`
reads it from stdin.

For the bearer hook: `make inbound-credential CRED_INBOUND=inbound-bearer INBOUND_SECRET=kaimahi-inbound-token`
stores the token as a Secret in the `kaimahi` namespace, and
`make inbound-fire HOOK=demo-bearer AUTH=bearer` uses it.

## Slack Events: wired, not live-verified

The `slack-events` hook implements Slack's request signing, the
`url_verification` handshake, and `event_callback` envelopes keyed by
`event_id`, all unit-tested against Slack's documented scheme. It has not
been exercised against Slack. Two reasons, both honest: Slack has to reach
a public HTTPS URL, and a kind cluster does not have one; and enabling
event subscriptions on the Slack app is a workspace-side change that
nobody in a worker session should make unasked.

To run it for real: expose `kaimahi-inbound` on a public HTTPS URL, paste
the app's signing secret with `make inbound-secret HOOK=slack-events`,
issue `make inbound-credential CRED_INBOUND=inbound-slack`, set the app's
Request URL to `https://<your host>/hook/slack-events` (Slack will send
the challenge; the plane answers it once the secret is projected),
subscribe to the events you want, then approve the hook with a bound.
Slack retries a delivery it did not get a 2xx for; after an event is
admitted, its retries are replays and get 409, which Slack gives up on
after three tries.

Socket Mode, the alternative that needs no public URL, would mean a
long-lived WebSocket client in the plane and a second token to keep. It
is the natural follow-up and was left out here deliberately.

## What this does not do

- No durable queue: the plane runs one replica, admitted-but-not-yet-run
  events die with a restart, and their `admitted` rows say so.
- The rate limiter is per replica, in memory.
- Sessions on the kagent side are attributed to the hook
  (`x-user-id: kaimahi-inbound/<hook>`), not to whoever sent the event.
- The plane's new egress to the kagent controller on 8083 is a path the
  NetworkPolicy work has to allow explicitly.
- No TLS termination, no public exposure: that is the operator's layer.
