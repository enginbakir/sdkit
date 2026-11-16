// Package proc runs child processes with context timeouts. Nothing here exits the process.
package proc

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Result is the outcome of a finished command. Code is -1 when the command could not start.
type Result struct {
	Stdout, Stderr string
	Code           int
}

// Run runs name with args in dir. env entries (KEY=VALUE) are appended to the current environment.
// A non-zero exit is reported in Result.Code, not as an error; err is set only when the
// command could not start or the context ended it.
func Run(ctx context.Context, dir string, env []string, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return finish(cmd, dir, env)
}

func finish(cmd *exec.Cmd, dir string, env []string) (Result, error) {
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	setProcAttr(cmd)
	err := cmd.Run()
	res := Result{Stdout: out.String(), Stderr: errb.String()}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee) && ee.ExitCode() >= 0:
		res.Code, err = ee.ExitCode(), nil
	default:
		res.Code = -1
	}
	return res, err
}

// Shell returns the command that runs a user command string. SDKIT_SHELL names the shell
// (a name or a path); see shellArgv for the flag each one gets. Unset: sh -c on Unix; on
// Windows sh when it is on PATH, else powershell, else cmd.
func Shell(ctx context.Context, cmdString string) *exec.Cmd {
	sh := strings.TrimSpace(os.Getenv("SDKIT_SHELL"))
	if sh == "" {
		sh = "sh"
		if runtime.GOOS == "windows" {
			for _, c := range []string{"sh", "powershell", "cmd"} {
				if _, err := exec.LookPath(c); err == nil || c == "cmd" {
					sh = c
					break
				}
			}
		}
	}
	argv := shellArgv(sh, runtime.GOOS)
	cmd := exec.CommandContext(ctx, argv[0], append(argv[1:], cmdString)...)
	shellAttr(cmd, argv, cmdString)
	return cmd
}

// shellArgv maps a shell name or path to its argv prefix (the command string is appended).
// Only Windows distinguishes shells: powershell/pwsh take -NoProfile -Command and cmd takes /C.
// Everything else, including an unknown name, is treated as a POSIX shell with -c, because
// that is what sh, bash, zsh, dash and busybox accept and it is the safest guess for a
// shell we do not know. Both \ and / separate path parts so a Windows path works on any host.
func shellArgv(shell, goos string) []string {
	base := shell[strings.LastIndexAny(shell, `\/`)+1:]
	base = strings.TrimSuffix(strings.ToLower(base), ".exe")
	if goos == "windows" {
		switch base {
		case "powershell", "pwsh":
			return []string{shell, "-NoProfile", "-Command"}
		case "cmd":
			return []string{shell, "/C"}
		}
	}
	return []string{shell, "-c"}
}

// RunShell runs a user command string through Shell.
func RunShell(ctx context.Context, dir string, env []string, cmdString string) (Result, error) {
	return finish(Shell(ctx, cmdString), dir, env)
}

// LookPath finds an executable on PATH (honours PATHEXT on Windows).
func LookPath(name string) (string, error) { return exec.LookPath(name) }
