# 0012. Agentless monitoring: one command per sample, a hub that tells the window only what changed

Status: Accepted

## Context

M3 shows CPU, memory, disk and GPU in every block's footer and a monitor page
per host (DESIGN §4.5). Nothing may be installed on the servers. Nine hosts
must cost the client about 0% CPU, and the window must not be redrawn every
second because numbers move. Remote cost must stay under 1% of a core.

## Decision

- **One command per sample** over the block's existing SSH session (an exec
  channel on the connection the shell uses; no new connection, no new login).
  The command prints `##SECTION` blocks; a pure function parses it
  (`Collector.Parse(out, prev, at)`), so every operating system is a parser
  test with a fixture. Counters (CPU jiffies, disk and network bytes) travel in
  `Raw` from one parse to the next; the first sample has no rates.
  - Linux: `/proc/stat`, `/proc/meminfo`, `/proc/loadavg`, `/proc/diskstats`,
    `/proc/net/dev`, `df -P`, `nvidia-smi` or `rocm-smi` when present. The
    command is wrapped in `sh -c '…'` so any login shell works.
  - macOS: `top -l 1`, `vm_stat`, `iostat`, `netstat -ib`, `df`.
  - Windows: one PowerShell `-EncodedCommand` that reads CIM/performance
    classes and prints JSON; it works under both `cmd.exe` and PowerShell
    login shells.
- **The machine is probed** with `uname -s` (an error that says the command
  exited with a status means Windows; other errors mean "not connected yet" and
  retry). The host's saved OS is only a hint: a Linux box saved as macOS would
  never produce a reading.
- **`Sampler`** is one goroutine per host: jitter (so nine hosts do not fire
  together), pause, exponential backoff after failures, a short gap after the
  first sample so rates appear quickly. **`Series`** keeps 3600 samples (one
  hour at 1 s).
- **`Hub`** owns the samplers and tells the window (`OnChange`) only when the
  footer would look different: a reading appears or fails, or one moves by at
  least three points from what was last shown (`HubOptions.Step`); changes are
  merged into at most one call per second (`MinGap`). The focused host (the
  monitor page) and failures are never held back. A busy shared machine's CPU
  wobbles by a point or two every sample; before this rule nine hosts caused
  5.7 whole-window frames per second and 2.8% CPU, after it 0.00–0.07 frames per
  second and 0.5%.
- Interval (default 2 s), warn (70) and critical (90) thresholds and an off
  switch are settings and apply live.

## Fixtures: what is recorded and what is not

Linux fixtures are recordings from a real machine. The macOS, Windows, NVIDIA
and ROCm fixtures are **composed by hand** in the exact formats the tools print,
from documentation and published output; no such machine was available. Parser
tests pass on them, but they prove the parsers match the documented formats,
not that every real machine prints exactly that. Before a release they are to
be replaced with recordings (`Collector.Command()` on a real host, saved
verbatim); the tests do not change. `internal/monitor/testdata/README.md` says
the same.

## Consequences

- Sampling cost is one `sh` that reads a few files: measured 4.3 ms of CPU per
  sample on the remote, 0.22% of one core at 2 s (it excludes sshd's own work
  for the channel).
- The footer can lag the truth by up to two points and one second; the page for
  the focused host does not.
- A server without `/proc` (BusyBox without procfs, exotic Unix) shows dashes.
