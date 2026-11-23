# 0002. Ports-and-adapters package layout

Status: Accepted (M1)

## Context

The risky parts of Tako (reconnecting SSH, input fan-out, persistence) must be
testable without a window, and the UI toolkit is a 0.x dependency.

## Decision

A flat ports-and-adapters layout under `internal/`, no DDD layers:

```
ui ─▶ app ─▶ workspace, session, hostlib, settings, inputbus, layout, model
              │
              ├─ transport        (port: Transport, Shell, Prompter)
              │   └─ transport/ssh   (adapter: golang.org/x/crypto/ssh)
              ├─ secrets          (port + encrypted file)
              └─ store            (atomic JSON documents, lock, backups)
```

- Interfaces live next to their user and stay small.
- `internal/app` is the only place that wires objects, by hand-written
  constructor injection. No DI framework.
- Core packages never import `ui`, `app`, MyGo or `golang.org/x/crypto/ssh`;
  `ui` never imports `x/crypto/ssh`. `depguard` checks this in CI.

## Consequences

Core packages are tested with an in-process SSH server (`testutil/memssh`),
fake proxies (`testutil/nettest`) and a real OpenSSH (`testutil/realsshd`);
core coverage is gated at 80%. Swapping the SSH library or the UI toolkit
touches one package.
