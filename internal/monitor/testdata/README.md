# Fixtures

- `linux-*.txt`: **recorded** from a real Linux machine with the collector's own
  command (`linux-2` after a burst of CPU load).
- `darwin-*.txt`, `windows-*.json`, `nvidia-*.txt`, `rocm-*.json`: **composed by
  hand** in the exact formats the tools print (from their documentation and
  from published outputs). No macOS, Windows or GPU machine was available when
  they were written, so they are not recordings. Before a release, replace
  them with real recordings (run `Collector.Command()` on a real host and save the output)
  and keep the tests unchanged.
