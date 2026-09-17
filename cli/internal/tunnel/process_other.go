//go:build !unix

package tunnel

import "os/exec"

func ownProcessGroup(*exec.Cmd) {}

// terminate kills the process: graceful signals aren't portable outside Unix.
func terminate(cmd *exec.Cmd) error { return cmd.Process.Kill() }

func kill(cmd *exec.Cmd) error { return cmd.Process.Kill() }
