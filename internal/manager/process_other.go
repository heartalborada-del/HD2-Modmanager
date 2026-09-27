//go:build !windows

package manager

import "os/exec"

func hideProcessWindow(*exec.Cmd) {}
