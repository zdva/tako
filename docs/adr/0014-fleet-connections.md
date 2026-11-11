# 0014. Transfers get their own connection; monitoring shares the block's session

Status: Accepted

## Context

Transfers move gigabytes and open an SFTP channel; monitoring runs a short
command every couple of seconds. A shell must stay responsive while a copy
saturates a link, and a copy must not die when the user closes a block.

## Decision

- **Transfers and the file browser** use a connection of their own per host,
  opened on first use by `app.fleet` with the same dialer (keys, jump hosts,
  proxies, host-key policy, prompts) and kept for later jobs. It is redialled
  when it fails (`Provider.Reset`), and closed when the app quits. The shell's
  channels never share a TCP stream with bulk data.
- **Monitoring** reuses the block's session (`Block.Session()`); sessions are
  already shared per host. A host with several blocks is measured once.
  Closing the last block of a host forgets it.
- OpenSSH's `MaxSessions` (default 10 channels per connection) is not a limit
  here: the fleet connection opens one SFTP channel and a few exec channels.

## Consequences

A host has up to two connections (shell and fleet). A copy to a host whose
block is closed still works; its first job dials again.
