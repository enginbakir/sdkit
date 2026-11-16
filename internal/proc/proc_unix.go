//go:build !windows

package proc

import (
	"os"
	"os/exec"
	"syscall"
)

// shellAttr is a no-op on Unix.
func shellAttr(*exec.Cmd, []string, string) {}

func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return KillTree(cmd) }
}

// Alive reports whether a process with pid exists (kill -0).
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// KillTree kills the command's process group.
func KillTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return cmd.Process.Signal(os.Kill)
	}
	return nil
}
