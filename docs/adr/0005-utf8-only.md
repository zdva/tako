# 0005. UTF-8 only

Status: Accepted (M1), final (user decision, M4)

## Context

Legacy servers may use GBK, Big5 or Shift-JIS. Supporting them means a
per-host encoding, transcoding in the terminal path and a lot of edge cases
(split multi-byte sequences, input encoding).

## Decision

Tako speaks UTF-8 on every connection. There is no encoding setting. The
status bar shows `UTF-8`.

## Consequences

Hosts that only produce GBK show garbled text; the answer is to configure the
server locale. If real demand appears this ADR is superseded by a per-host
encoding option implemented in `session`, not in the UI.

## Update (M4)

The product owner has decided that GBK and other non-UTF-8 encodings are out
of scope for good: there is no encoding setting, no transcoding layer and no
plan for one. Any reintroduction needs a new ADR.
