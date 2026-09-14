//go:build !windows

package runner

import (
	"os/exec"
	"syscall"
)

func configureProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func terminateProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}

	pid := cmd.Process.Pid

	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
