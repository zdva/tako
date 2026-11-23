// Package hostfile imports hosts from other terminals' files: Xshell
// sessions (.xsh, or an .xts/.zip archive or a folder of them), SecureCRT
// sessions (.ini files or an exported .xml) and a Termius CSV, and reads
// Tako's own export.
//
// Parsing is a pure function of the bytes: it never touches the secret store or
// the network. Passwords in these files are encrypted with the other
// program's keys and are never imported or decoded; the result says how many
// hosts had one so the user can type them again. Anything that cannot be
// represented (telnet and serial sessions, proxies, keys that live in the
// other program's store) becomes a warning instead of a silent change.
//
// The formats were read from public documentation and sample files, not from
// every version of the programs; see docs/IMPORT.md for what is covered.
package hostfile
