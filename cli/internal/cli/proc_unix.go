//go:build unix

package cli

import (
	"os/exec"
	"syscall"
)

// configureProcess starts the app in its own process group, so a terminal
// Ctrl+C reaches only orb, which then stops the app exactly once.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func interrupt(cmd *exec.Cmd) error {
	return cmd.Process.Signal(syscall.SIGINT)
}
