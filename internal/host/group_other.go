//go:build !unix

package host

import (
	"os"
	"os/exec"
)

// setGroup has no process groups to use here.
func setGroup(*exec.Cmd) {}

// killGroup kills the process itself.
func killGroup(p *os.Process) error {
	return p.Kill()
}
