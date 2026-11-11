// Package monitor measures a remote machine without installing anything on
// it: one command per sample is run over the SSH connection, and its output
// is parsed here. Collectors exist for Linux (/proc and df, with nvidia-smi or
// rocm-smi when present), macOS (top, vm_stat, iostat, netstat) and Windows
// (PowerShell and CIM).
//
// A [Collector] only builds the command and parses its output; both are pure,
// so the parsers are tested with recorded outputs. A [Sampler] runs a
// collector against one host on a timer, and a [Series] keeps the history the
// monitor page draws.
package monitor
