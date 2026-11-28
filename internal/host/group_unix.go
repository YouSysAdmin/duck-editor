//go:build unix

package host

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// setGroup starts cmd as the leader of a new process group.
func setGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}

	cmd.SysProcAttr.Setpgid = true
}

// killGroup kills the process group led by p.
func killGroup(p *os.Process) error {
	err := syscall.Kill(-p.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return p.Kill()
	}

	return err
}
