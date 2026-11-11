//go:build !unix

package realsshd

import "os/exec"

func killGroup(cmd *exec.Cmd) { _ = cmd.Process.Kill() }
