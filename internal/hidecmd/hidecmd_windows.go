package hidecmd

import (
	"os/exec"
	"syscall"
)

// Hide sets SysProcAttr to hide the console window on Windows.
// Call on any exec.Cmd before .Run() / .Output() / .CombinedOutput().
func Hide(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	return cmd
}

// Command creates an exec.Cmd with hidden window already set.
func Command(name string, arg ...string) *exec.Cmd {
	return Hide(exec.Command(name, arg...))
}
