//go:build windows

package runner

import "os/exec"

func configureProcessGroup(cmd *exec.Cmd) {}

func terminateProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
