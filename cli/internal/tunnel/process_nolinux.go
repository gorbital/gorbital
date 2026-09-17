//go:build unix && !linux

package tunnel

import "syscall"

// setParentDeath does nothing where the kernel has no parent-death signal:
// orb dev stops cloudflared on every exit it handles (return, Ctrl+C,
// SIGTERM), not when it is killed with SIGKILL.
func setParentDeath(*syscall.SysProcAttr) {}
