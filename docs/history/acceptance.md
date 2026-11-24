# 验收记录

从 [CHANGELOG.md](../../CHANGELOG.md) 移出的验收表。每一节都是**那个标签当时**在一台 Linux 虚拟机上的记录（Xvfb，Mesa 软件渲染，对应该标签的干净检出）。数字不是对当前 `main` 的承诺，也没有在 macOS 或 Windows 真机上复测。

功能变化仍写在 CHANGELOG 对应版本下。

## v0.4.0（2026-11-11）

| # | Criterion | Result |
|---|---|---|
| 1 | Installers install, start, connect and update on clean systems of all three platforms | **Not met, needs people and machines.** On this Debian 13 box the `.deb` installed, started and uninstalled cleanly, and the `.tar.gz`, PKGBUILD and `.rpm` (nfpm) build; a signed update manifest is produced with a throw-away key. The dmg and NSIS installers are built by CI only (no `makensis` or macOS here) and the updater has never installed an update end to end |
| 2 | Light/dark switch without a restart; `theme` tokens equal `DESIGN.md` | Pass: `TestTokensMatchDesignDoc`, `TestSettings_DarkAppearanceChangesTerminalToo`, `TestSettings_SchemeRecolorsOpenTerminalsWithoutARestart` |
| 3 | English and Chinese complete; pseudo-locale shows no missing key, hard-coded text or text outside the window | Pass: `TestKeys_MatchLocales`, `TestPseudoLocale_EverySurfaceComesFromTheTables` (main window, host sheet, palette, every settings page, transfer sheet). Truncation is checked as "inside the window", not by pixel |
| 4 | An export imported on another computer works and contains no secret (grep) | Pass: `TestSettings_ExportHasNoSecretsAndImportsOnAnotherComputer`, `TestExportImportMovesHostsToAnotherComputerWithoutSecrets` (jump chain remapped) |
| 5 | Performance budgets; `goleak` and `-race` green | Pass here: nine idle blocks 0.40% of a core, 129 MB RSS (35 MB private), monitored 0.93% and 0.00 frames/s, flood 59 frames/s, cold start 0.14 s, recovery after `kill -9` 0.29 s; core coverage 83.3%; `make race` and `make vuln` green; five fuzz targets run 20 s each |
| 6 | Manual checklist on a real Mac and a real Windows | **Not done**: no such machines ([RELEASE.md](../RELEASE.md)) |
| 7 | Linux smoke on Ubuntu 22.04/24.04 and Fedora | **Not done**: only Debian 13 was available |

## v0.3.0-m3（2026-11-11）

| # | Criterion | Result |
|---|---|---|
| 1 | Readings within 5 points of the system tools; parser tests for all three systems | Linux: pass against the real sshd (CPU 13.6% vs `/proc/stat` 13.5%, 47.7% vs 47.7% under load; memory equal). macOS, Windows, NVIDIA, ROCm: parsers pass on **hand-composed** fixtures only; **not validated on real machines or GPUs** |
| 2 | Remote cost < 1%; client idle ≈ 0%; no whole-window redraw every second | Remote: 4.2 ms CPU per sample = 0.21% of a core at 2 s. Client, nine hosts at 2 s: 0.6–0.7% of one core (0.00 frames/s); monitoring off: 0.00% |
| 3 | 2.4 GB relay: ≥ 80% of the link, nothing on local disk, memory growth < 32 MB | Pass: 2400 MiB over 100 MB/s links: 100 MB/s vs 99 MB/s raw SFTP = 100%; heap growth 12.7 MiB; 0 KiB written by the client. Unthrottled loopback (informational): 69% on a shared CPU |
| 4 | Link cut mid-transfer: Paused, resumes from the break, verified; also for folders | Pass (file: paused at 160 of 400 MiB, resumed from 144 MiB; folder likewise) |
| 5 | Direct mode only when enabled; no key left on the server; forwarding off afterwards | Pass for `scp` against a real sshd; the `rsync` branch is covered with a fake environment only (no rsync on the box) |
| 6 | Overwrite / skip / ask policies | Pass (unit and integration on a real SFTP server) |
| 7 | Monitor page and transfer sheet match `monitor.png` / `transfer.png` in structure | Pass in `ui.Tester` structure tests; screenshots in `docs/screenshots/m3/` |

同一节里记下的测量和限度：

- Monitoring costs the client about 0.6% of a core with nine hosts at 2 s (software SSH crypto and parsing); it is not exactly zero.
- The shape of the 2.4 GB test depends on the loopback; real links are unmeasured. GPU-backed rendering was unmeasured on macOS and Windows.

## v0.2.0-m2（2026-11-11）

| # | Criterion | Result |
|---|---|---|
| 1 | 9 synced blocks get identical bytes, no cross-replies; a 10 KB/s target does not delay the other 8 | Pass (`inputbus` acceptance tests, StripReplies fuzz; real window: one command ran in all 9 blocks) |
| 2 | Minimap click, box (⇧/⌥), reorder, corner snap, opacity in `ui.Tester`; translucent vs opaque by screenshot | Pass (Tester tests; e2e compares real-window pixels: 94% of the header pixels change between rest and hover) |
| 3 | Divider snapping ½/⅓/⅔ with guide; layout property tests | Pass |
| 4 | Disconnected block's sync pauses, others unaffected, resumes | Pass |
| 5 | Resize and layout drag debounce terminal resizes | Pass: 60 pointer moves, 2 blocks, 2 window-size changes, none during the drag |
| 6 | Restart restores manual layout and sync state | Pass (Tester; real window with `kill -9`) |
| 7 | Shortcuts match the DESIGN table | Pass (test parses DESIGN.md §4.6) |
| 8 | 9-block flood ≥ 18 fps | Pass: 59 frames/s, mean frame 3.5 ms (1440×900, software renderer) |

Also, on that run: idle with 9 blocks CPU 0.00%, private memory 24 MB (RSS 71 MB), cold start 0.15 s, `kill -9` recovery 0.21 s; core coverage 85.0%; `govulncheck` clean; lint clean.

## v0.1.0-m1（2026-11-11）

| # | Criterion | Result |
|---|---|---|
| 1 | Auth × jump × proxy matrix; vim/top usable | Pass for key, agent and PPK against a real OpenSSH; password and keyboard-interactive against the in-process server (an unprivileged sshd cannot do PAM) |
| 2 | Outage: banner, scrollback kept, no EOF, input not replayed | Pass in the integration test (relay paused, then cut; the outage is shorter than 60 s and the backoff is shortened, the mechanism is the same) |
| 3 | Changed host key refused with a clear message | Pass |
| 4 | `~/.ssh/config` import ≥ 12 hosts | Pass (14-host fixture with Include, ProxyJump, IdentityFile) |
| 5 | 9 idle blocks: 0% CPU, RSS ≤ 100 MB, cold start ≤ 1 s | CPU 0.00% over 10 s; cold start: window in 0.13–0.15 s, 9 blocks connected in 0.19–0.22 s (0.5–0.6 s on a first run with an empty fontconfig cache). **RSS depends on the Xvfb setup:** 63.8 MB on a plain long-running Xvfb, 116.5 MB under `xvfb-run` where Mesa loads LLVM (87 MB of that is shared libraries; private memory is 21–30 MB). The e2e test asserts private memory ≤ 100 MB and logs total RSS. Total RSS on a real GPU is unmeasured (see 9) |
| 6 | Restore after quit; `kill -9` recovers ≤ 1 s | Pass; window 0.14 s, 9 reconnected 0.20 s |
| 7 | Secrets absent from files, logs, export | Pass (grep test) |
| 8 | Race, lint, govulncheck green; coverage ≥ 80% | Pass; core coverage 83.5% |
| 9 | GPU baseline on real macOS/Windows | **Not done** (no such hardware here) |
