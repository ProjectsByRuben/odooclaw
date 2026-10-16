//go:build windows

package providers

import (
	"os/exec"
	"strconv"
)

func prepareQuotaCommand(cmd *exec.Cmd) {
	// no-op on Windows
}

func terminateQuotaCommand(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	pid := cmd.Process.Pid
	if pid <= 0 {
		return nil
	}

	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
	_ = cmd.Process.Kill()
	return nil
}
