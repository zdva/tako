# 0015. Direct server-to-server copies: agent forwarding for one command, then gone

Status: Accepted

## Context

Relaying through this computer needs no trust between the servers but pays for
two trips. Letting the destination pull straight from the source is faster when
the servers are close, but the destination must authenticate to the source
without holding the user's private key.

## Decision

- Direct mode is **off by default** and available only after the user turns it
  on in Settings; the transfer sheet refuses it otherwise, and so does the
  queue.
- The destination runs `rsync` (when both servers have it; `--partial
  --append-verify` resumes) or `scp`, authenticating through the **user's SSH
  agent, forwarded for that one command**. OpenSSH accepts only one
  `auth-agent-req` per connection, so the command runs on a **separate
  connection that is closed afterwards**; closing it ends the forwarding. The
  command requests a pty so the remote command gets SIGHUP if the channel goes.
- The destination has never met the source's host key. Tako writes a one-line
  `~/.tako-kh-<random>` containing only the key it already trusts for the
  source, passes it with `StrictHostKeyChecking=yes` and `UserKnownHostsFile`,
  and removes the file afterwards. No private key is written anywhere.
- Progress is read by polling the size of the destination's part file.
- The result is checked by size (file) or file count and total size
  (directory), and by SHA-256 on both servers when asked.

## Consequences

- With no agent, or an agent without the source's key, direct mode fails with
  the remote's message (`Permission denied (publickey)`); relay still works.
- Direct directory copies use `scp -r` and have no part-directory resume (a
  stopped job starts the directory again); a single file resumes with rsync.
- The rsync branch is exercised by unit tests with a fake environment, not by
  the integration suite (the box has no rsync).
