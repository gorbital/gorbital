//go:build unix

package tunnel

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
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

// adoptSupported reports that this platform can identify a cloudflared an
// earlier orb dev left running, and stop it (see [Manager.stopLeftover]).
const adoptSupported = true

// psTimeout bounds the ps call that describes a process.
const psTimeout = 3 * time.Second

// lstartFields is how many fields ps prints for lstart ("Wed Sep 17
// 12:34:56 2026").
const lstartFields = 5

// processIdentity returns the start time and command line the system
// reports for pid, and whether the process exists at all. The pair
// identifies a process: a PID reused by another program has another start
// time. Everything is empty and ok is false when the process is gone or ps
// can't answer, which callers must treat as "unknown", never as a match.
func processIdentity(pid int) (started, command string, ok bool) {
	if pid <= 0 {
		return "", "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), psTimeout)
	defer cancel()
	// POSIX ps, on macOS and Linux alike: lstart is the start time, args
	// the command line. A process that doesn't exist prints nothing.
	out, err := exec.CommandContext(ctx, "ps", "-o", "lstart=,args=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", "", false
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	fields := strings.Fields(line)
	if len(fields) <= lstartFields {
		return "", "", false
	}
	return strings.Join(fields[:lstartFields], " "), strings.Join(fields[lstartFields:], " "), true
}

// leftoverAlive reports whether pid still exists. A PID orb can see but not
// signal counts as alive: it is not orb's to stop.
func leftoverAlive(pid int) bool {
	return pid > 0 && !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

// signalLeftover stops a process an earlier orb dev left: SIGTERM, or
// SIGKILL when hard. With group it signals the process group pid leads, so
// everything cloudflared started goes too.
func signalLeftover(pid int, group bool, hard bool) error {
	if pid <= 0 {
		return errors.New("no process")
	}
	sig := syscall.SIGTERM
	if hard {
		sig = syscall.SIGKILL
	}
	target := pid
	if group {
		target = -pid
	}
	if err := syscall.Kill(target, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}
