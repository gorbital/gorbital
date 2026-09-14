//go:build !unix

package cli

import "os/exec"

func configureProcess(*exec.Cmd) {}

// interrupt kills the process: graceful interrupts aren't portable outside Unix.
func interrupt(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}
