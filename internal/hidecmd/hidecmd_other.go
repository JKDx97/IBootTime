//go:build !windows

package hidecmd

import "os/exec"

// Hide is a no-op on non-Windows platforms.
func Hide(cmd *exec.Cmd) *exec.Cmd { return cmd }

// Command creates a normal exec.Cmd on non-Windows platforms.
func Command(name string, arg ...string) *exec.Cmd {
	return exec.Command(name, arg...)
}
