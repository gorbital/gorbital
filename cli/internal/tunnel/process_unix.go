//go:build unix

package tunnel

import (
	"errors"
	"os/exec"
	"syscall"
)

// ownProcessGroup starts cloudflared in a process group of its own: a
// terminal's Ctrl+C reaches only orb, which then stops the whole group, so
// nothing cloudflared starts outlives orb dev. On Linux the group's leader
// also dies with orb (setParentDeath).
func ownProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	setParentDeath(cmd.SysProcAttr)
}

// terminate asks the group to stop (SIGTERM).
func terminate(cmd *exec.Cmd) error { return signalGroup(cmd, syscall.SIGTERM) }

// kill ends the group (SIGKILL).
func kill(cmd *exec.Cmd) error { return signalGroup(cmd, syscall.SIGKILL) }

func signalGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd.Process == nil {
		return errors.New("not started")
	}
	// A negative PID signals the process group the child leads.
	if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return cmd.Process.Signal(sig)
	}
	return nil
}
