# Importing hosts from other terminals

Settings → Data → **Import hosts** reads a file or a folder and adds the hosts
in it. Tako never touches the other program, never decodes its passwords and
never changes a host that already exists (a host with the same name, address,
port and user is skipped and counted).

| Source | What to select | What is read |
|---|---|---|
| Xshell | a `.xsh` file, an `.xts` / `.zip` session archive, or the `Sessions` folder | Host, port, user, authentication method; folders become tags. Xshell 5+ (`[CONNECTION]`) and 4 and earlier (`[General]`) layouts. UTF-8 and UTF-16 files. |
| SecureCRT | a folder of session `.ini` files, or the XML export (File → Export) | Hostname, port, username, protocol, identity file, firewall / jump session name; folders become tags. |
| Termius | the CSV Termius exports and imports | Groups, Label, Tags, Hostname/IP, Protocol, Port, Username. Columns are found by name, in any order. |
| Tako | a file written by Settings → Data → **Export hosts** | Hosts, groups, tags, jump chains. |
| OpenSSH | `~/.ssh/config` (the welcome screen and the host sheet) | Unchanged since M1; see SPEC. |

## What is never imported

- **Passwords and passphrases.** They are encrypted with the other program's
  keys. The summary says how many saved passwords were left out ("N saved
  passwords were not imported; enter them when you connect"); Tako asks on first
  connect and offers to keep the password in the system keychain.
- **Keys kept in the other program's own store.** The host is imported with a
  warning; choose the key file in *Edit host*.
- **Telnet, serial, RDP, Mosh and local-shell sessions.** Skipped with a
  warning that names the session.

Anything that cannot be represented becomes a warning (the first five are shown,
the rest are counted) instead of a silent change.

## Export and moving to another computer

*Export hosts* writes `tako-hosts.json` with mode 0600: hosts, groups, tags,
jump chains and per-host settings. It contains **no password, no passphrase and
no keychain reference** (an automated test greps the file for the secrets and
the references it just stored). On the other computer, import the file; IDs and
jump chains are remapped, and each host asks for its password the first time.
Key *paths* are kept as written, so a path that exists only on the old
computer needs to be chosen again.

## Safety limits

Files are untrusted. A single file is limited to 32 MB, a folder or archive to
128 MB, 20 000 files and 100 000 hosts; archives are read in memory (no path is
ever written to disk) and a path that climbs out of the archive is ignored.
`FuzzParse` (hostfile) and `FuzzParseITerm` (colors) run in `make fuzz`.

## Terminal colors

*Terminal → Import iTerm2 colors* reads an `.itermcolors` file (a property list)
and adds it as a scheme. Ghostty, Alacritty and other formats are not read.

## Caveat

The parsers were written from the programs' public documentation and sample
files. No real Xshell, SecureCRT or Termius was available while building them;
the fixtures in `internal/importer/hostfile/testdata` are hand-written.
Replacing them with real exports is on the pre-release checklist
([RELEASE.md](RELEASE.md)).
