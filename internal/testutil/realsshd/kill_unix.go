//go:build unix

package realsshd

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// killGroup kills sshd and every process descended from it: the privileged
// monitor and the session processes OpenSSH forks per connection.
func killGroup(cmd *exec.Cmd) {
	out, err := exec.Command("ps", "-e", "-o", "pid=,ppid=").Output()
	children := map[int][]int{}
	if err == nil {
		for line := range strings.Lines(string(out)) {
			f := strings.Fields(line)
			if len(f) != 2 {
				continue
			}
			pid, err1 := strconv.Atoi(f[0])
			ppid, err2 := strconv.Atoi(f[1])
			if err1 == nil && err2 == nil {
				children[ppid] = append(children[ppid], pid)
			}
		}
	}
	var kill func(int)
	kill = func(pid int) {
		for _, c := range children[pid] {
			kill(c)
		}
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	kill(cmd.Process.Pid)
}
