# DESIGN: Connection authentication for lazyreckon

**Status:** Draft — not yet implemented (deliberately deferred; see Sequencing).
**Date:** 2026-05-29
**Repos touched (eventual):** `reckon-gateway`, `reckon-proto`, `reckon-go`, `reckon-lazy`

---

## The question this answers

> "lazyreckon lets you connect to various clusters. To connect, the user
> needs the cookie, which acts like a password — so the 'new cluster'
> dialog should have a cookie field. Right?"

Half right. A connection **should** carry a credential. But the Erlang
**cookie is the wrong credential, in the wrong place**, and putting it in
lazyreckon would violate the gateway's deliberate security boundary.

---

## Why the cookie does not belong in lazyreckon

The connection is **two hops with two different auth planes**:

```
lazyreckon ──gRPC (TCP, currently INSECURE)──▶ reckon-gateway ──Erlang dist (COOKIE)──▶ reckon-db cluster
   hop 1: client → gateway                          hop 2: gateway → backend cluster
```

- **Hop 2 (cookie).** The Erlang cookie authenticates the gateway's
  Erlang-distribution link to the backend cluster. It is a *server-side,
  operator-curated* secret:
  - lives in `clusters.eterm` on the gateway host (chmod 0600, outside any
    gitops repo);
  - used only by `reckon_gateway_cluster_connector` (`erlang:set_cookie` +
    `net_kernel:connect_node`);
  - `redact`-ed in logs and **never returned over the gRPC API**
    (`GetCatalogueStatus` strips it).
  The catalogue is curated by file, on purpose — cookies never traverse
  the wire.

- **Hop 1 (where lazyreckon lives).** `reckon-go`'s `Connect` dials with
  `insecure.NewCredentials()` (`reckon-go/client.go:34`). lazyreckon is a
  Go gRPC client: it has **no Erlang node**, so it categorically *cannot*
  present an Erlang cookie (gRPC has no such concept).

### Consequences

1. What lazyreckon calls a **"cluster" is really a gateway endpoint.**
   Under catalogue mode, one endpoint federates *N* clusters' stores. The
   per-cluster cookies are the gateway operator's secret, not the viewer's.
2. A viewer should **never** need the backend dist cookie. Requiring it
   would push a server-side dist credential out to every viewer and over
   the wire — exactly what the gateway authors prevented.
3. Concrete illustration: the `PARKSIM_COOKIE` needed to *deploy* the
   parksim fleet is this hop-2 secret. The operator needs it; a lazyreckon
   viewer never should.

---

## The real gap, and the right credential

Hop 1 is currently unauthenticated: anyone who can reach
`beam00.lab:50051` reads every store. So the instinct ("connections need a
password") is correct — but the credential is a **gateway client
credential**, not the cookie:

| | Naive proposal | Correct design |
|---|---|---|
| Field in "new connection" dialog | Erlang cookie | Gateway **auth token** (or path to mTLS client cert) |
| Stored where | profiles.toml | **OS keyring**, keyed by endpoint (profiles.toml stays secret-free) |
| Authenticates | gateway → cluster (wrong hop) | client → gateway (the open hop) |

The codebase already names this future:
- `reckon-go/client.go:29` — *"Currently uses insecure transport. TLS +
  capability-token auth are [a follow-up]."*
- `reckon-lazy/internal/profiles/profile.go:5` — *"secrets (when the
  gateway gets auth) go in the OS keyring, not this file."*

`reckon-go`'s `Connect(ctx, endpoint, opts ...grpc.DialOption)` already
exposes the seam: the credential becomes a `grpc.PerRPCCredentials` or TLS
`grpc.DialOption`.

---

## Sequencing (no stubs)

Adding the field now would be a stub — the gateway has nothing to validate
it against. Honest order:

1. **reckon-gateway** — add a gRPC auth interceptor: validate a
   bearer/capability token, or require mTLS. (New `reckon-proto` / server
   work; decide token vs mTLS vs both.)
2. **reckon-go** — add a dial option carrying the credential
   (`grpc.PerRPCCredentials` or TLS creds) through the existing
   `opts ...grpc.DialOption` seam.
3. **reckon-lazy** — add the token field to the splash "new connection"
   form; store it in the OS keyring keyed by endpoint; keep profiles.toml
   secret-free. Optionally rename "cluster" → "connection" / "endpoint"
   to match catalogue reality.

---

## Terminology note

Consider renaming the lazyreckon concept **"cluster" → "connection" /
"endpoint."** One gateway endpoint federates many clusters under catalogue
mode, so "profile = cluster" is a misnomer; "profile = connection" is
accurate and avoids implying the viewer manages a cluster.

---

## Out of scope: operator catalogue curation

There **is** a place a cookie legitimately appears: an *operator* flow that
adds a cluster (members + cookie) to a gateway's catalogue — that edits
hop-2 config. The gateway keeps this **file-based and off the API** by
design (cookies never cross gRPC). Recommendation: keep catalogue curation
as an operator action on `clusters.eterm`. If it ever gets a UI, make it a
separate, clearly-labeled **operator/admin** mode — and recognise that
exposing it means consciously choosing to send dist cookies over the wire,
which this design advises against.

---

## Recommendation

Do **not** add a cookie field. When closing the hop-1 gap, add a *gateway
auth token* field backed by the OS keyring — **after** the gateway can
actually validate it. Until then, the connection stays unauthenticated and
that is a known, documented gap.
