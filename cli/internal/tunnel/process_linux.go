package tunnel

import "syscall"

// setParentDeath has the kernel send SIGTERM to cloudflared when orb dev
// dies, even with SIGKILL.
func setParentDeath(attr *syscall.SysProcAttr) { attr.Pdeathsig = syscall.SIGTERM }
