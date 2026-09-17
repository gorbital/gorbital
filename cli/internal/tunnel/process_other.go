//go:build !unix

package tunnel

import (
	"errors"
	"os/exec"
)

func ownProcessGroup(*exec.Cmd) {}

// terminate kills the process: graceful signals aren't portable outside Unix.
func terminate(cmd *exec.Cmd) error { return cmd.Process.Kill() }

func kill(cmd *exec.Cmd) error { return cmd.Process.Kill() }

// adoptSupported is false here: there is no portable way to prove a PID is
// still the cloudflared an earlier orb dev started, and orb never stops a
// process it can't identify. A record left behind is deleted unread.
const adoptSupported = false

func processIdentity(int) (started, command string, ok bool) { return "", "", false }

func leftoverAlive(int) bool { return false }

func signalLeftover(int, bool, bool) error { return errors.New("not supported on this platform") }
