# 0013. File transfers: a job queue, part files, resume with rewind, relay through a bounded pipe

Status: Accepted

## Context

Uploads, downloads and server-to-server copies of files and directories must
survive a lost connection, resume from where they stopped, verify what they
wrote, and never write a server-to-server stream to local disk. A 2.4 GB relay
must run near the link's speed with bounded memory.

## Decision

- **Endpoints** (`transfer.Endpoint`): an `FS` (local, or SFTP over
  `pkg/sftp` with concurrent reads and writes) plus a way to hash a file where
  it lives (`sha256sum`/`shasum` over exec, with a read-back fallback).
- **`Queue` / `Job`** with states queued, running, paused (reason: user or
  network), asking (same-name conflict), done, failed, canceled; a few jobs run
  at once (setting, default 3). A job is one source file or directory copied
  into a destination directory.
- **Part files.** A file is written to `name.tako-part` and renamed when it is
  complete and verified. A pause, a lost link or a quit leaves the part file;
  the next attempt continues from its size, aligned to 64 KiB. When the
  destination is remote the offset is stepped back 16 MiB first: SFTP's
  concurrent writes can leave holes before the end of a file that is longer
  than the data that really arrived.
- **Verification.** Size always. A resumed file is also hashed (source and
  destination, each where the file is); on a mismatch it restarts from zero
  once. The setting can force hashing (`hash`) or sizes only (`size`).
- **Automatic retry.** A network error pauses the job as "network" and a timer
  re-queues it, with the delay doubling up to 30 s, five times by default;
  then it waits for the user. Permission and similar errors fail at once. What
  counts as a network error is decided in one place (`isNetwork`): net errors
  that are timeouts or operation errors, and `ECONNRESET`, `ECONNABORTED`,
  `EPIPE`, `ETIMEDOUT`, plus a lost SSH connection.
- **Relay.** Remote to remote reads with `WriteTo` and writes with `ReadFrom`
  (both keep SFTP's parallel requests busy), joined by a bounded pipe of 8 ×
  256 KiB buffers. Nothing touches the local disk; heap growth is about 23 MiB
  whatever the file size.
- **Same-name policy** ask / overwrite / skip, for a job's files; a question can
  be answered for the rest of the job.
- Symbolic links and special files are skipped and counted. Modification times
  and permissions are not preserved (a limit, see the changelog).

## Consequences

- A relay between two links of equal speed runs at 101% of what one raw SFTP
  stream between the same two hosts achieves (rate-limited loopback links); on
  unthrottled loopback the CPU is shared and it reaches 51–66%, which is why the
  strict test uses rate-limited links.
- The queue is in memory: a quit forgets the list (the part files stay and a
  new transfer of the same file resumes).
- A resumed copy to a remote server transfers up to 16 MiB again.
