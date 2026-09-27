//go:build windows

package manager

import (
	"os/exec"
	"syscall"
)

func hideProcessWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
