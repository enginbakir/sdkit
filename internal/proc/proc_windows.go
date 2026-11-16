//go:build windows

package proc

import (
	"os/exec"
	"strconv"
	"syscall"
)

// shellAttr hands cmd.exe the string verbatim: Go's default argv quoting breaks cmd /C.
func shellAttr(cmd *exec.Cmd, argv []string, cmdString string) {
	if argv[len(argv)-1] == "/C" {
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `"` + cmd.Path + `" /S /C "` + cmdString + `"`}
	}
}

func setProcAttr(cmd *exec.Cmd) {
	cmd.Cancel = func() error { return KillTree(cmd) }
}

// Alive reports whether a process with pid exists.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	const access = syscall.PROCESS_QUERY_INFORMATION
	h, err := syscall.OpenProcess(access, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if syscall.GetExitCodeProcess(h, &code) != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}

// KillTree kills the command's process and its children.
func KillTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
