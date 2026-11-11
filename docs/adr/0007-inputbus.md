# 0007. Synchronized input through an InputBus

Status: Accepted (M1 mechanism, M2 UI)

## Context

Typing in one block can be mirrored to every synced block. The terminal also
*answers* some programs by itself (cursor position, device attributes, focus
and color reports). If those answers were mirrored, N terminals would answer a
query N times and loops would form.

## Decision

`internal/inputbus` owns the mirroring:

- Each block's terminal connection is wrapped in a `SyncConn`. User input is
  handed to the bus together with the block id; the bus writes it to the
  block's own pane and to the other synced blocks.
- Every target has its own queue and goroutine, so one stalled host never
  delays the others. A disconnected target is skipped, never buffered.
- Bytes the terminal generates as *replies* are recognised by a small
  escape-sequence classifier (`StripReplies` in `filter.go`) and are sent to
  the originating block only. Anything the classifier is unsure about is
  treated as user input.
- The bus never feeds a block's output back into the bus, so there are no loops.

## Consequences

The classifier is table-tested (`filter_test.go`) and the bus is tested for
ordering, isolation of a stalled target and absence of loops. In M1
the bus and the per-block sync toggle exist; the sync capsule, "all on / off"
and minimap selection are M2.
