# Architecture decision records

Each file records one decision that is expensive to reverse: the context, the
decision and what follows from it. New decisions get the next number; a
decision that is replaced is marked *Superseded by* and kept.

| # | Title | Status |
|---|---|---|
| [0001](0001-mygo-only-ui.md) | MyGo is the only UI toolkit | Accepted |
| [0002](0002-ports-and-adapters.md) | Ports-and-adapters package layout | Accepted |
| [0003](0003-secrets-in-keychain.md) | Secrets live in the system keychain only | Accepted |
| [0004](0004-host-key-policy.md) | Host-key policy: Tako's own known_hosts, never silently accept a change | Accepted |
| [0005](0005-utf8-only.md) | UTF-8 only | Accepted |
| [0006](0006-session-pane-contract.md) | `session.Pane` never reaches EOF while a link is down | Accepted |
| [0007](0007-inputbus.md) | Synchronized input through an InputBus | Accepted |
| [0008](0008-layout-model.md) | Layout model: auto grid now, split tree later | Accepted |
