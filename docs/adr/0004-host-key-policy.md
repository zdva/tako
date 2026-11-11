# 0004. Host-key policy

Status: Accepted (M1)

## Context

Silently trusting a changed host key defeats SSH. Reading and writing the
user's `~/.ssh/known_hosts` from a GUI app also surprises people.

## Decision

- Tako keeps its own OpenSSH-format `known_hosts` in its config directory and
  does not modify `~/.ssh/known_hosts`.
- Unknown key: policy `ask` (default) shows the SHA-256 fingerprint and
  remembers the answer; policy `strict` refuses unless the key is already known.
- **Changed key: always refused**, with a message that shows the old and new
  fingerprints and says what to do. There is no "accept anyway" button in M1;
  the user removes the old entry deliberately.
- Every hop of a jump chain is verified, not only the target.
- Preferred host-key algorithms follow what is already in `known_hosts`, so a
  server offering several key types does not trigger false alarms.

## Consequences

Integration test `TestHostKeyChangeRefusedAndExplained` covers the refusal and
its wording. Importing the user's existing known_hosts is a later, explicit
action.
