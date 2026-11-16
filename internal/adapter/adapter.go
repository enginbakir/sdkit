// Package adapter is the execution-adapter contract (docs/design-adapters.md): what each host supports, the command line of
// the five built-in hosts, and the optional external adapters. It never records a verdict: run-role does, on its own process.
package adapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	kit "github.com/wecii/sdkit"
)

// Hosts are the built-in hosts, in the order the docs list them.
var Hosts = []string{"claude", "codex", "opencode", "cursor", "gemini"}

// Keys are the eight capability keys and Values the legal values, in the order the adapters.json lists them.
var (
	Keys   = []string{"noninteractive", "tool_use", "model_selection", "usage_reporting", "structured_output", "cancellation", "timeout", "cost_reporting"}
	Values = []string{"help-verified", "declared", "estimated", "kit", "none"}
)

// Exit codes of the adapter command.
const (
	ExitOK, ExitInvalid, ExitUsage, ExitUnsupported = 0, 1, 2, 3
)

type kv struct{ K, V string }

// Builtin reports whether host is one of the hosts of core/adapters.json.
func Builtin(host string) bool {
	for _, h := range Hosts {
		if h == host {
			return true
		}
	}
	return false
}

// CLI is the executable name of a built-in host.
func CLI(host string) string {
	if host == "cursor" {
		return "cursor-agent"
	}
	return host
}

// orderedStrings reads a JSON object of scalars keeping the key order (jq does; a Go map would not).
func orderedStrings(raw []byte) ([]kv, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	if t, err := d.Token(); err != nil || t != json.Delim('{') {
		return nil, fmt.Errorf("not an object")
	}
	var out []kv
	for d.More() {
		k, err := d.Token()
		if err != nil {
			return nil, err
		}
		var v any
		if err := d.Decode(&v); err != nil {
			return nil, err
		}
		s, ok := v.(string)
		if !ok {
			b, _ := json.Marshal(v)
			s = string(b)
		}
		out = append(out, kv{k.(string), s})
	}
	return out, nil
}

// builtinCaps is the caps object of a built-in host as the file has it.
func builtinCaps(host string) ([]byte, bool) {
	b, err := kit.FS.ReadFile("core/adapters.json")
	if err != nil || strings.HasPrefix(host, "_") {
		return nil, false
	}
	var all map[string]json.RawMessage
	var e struct {
		Caps json.RawMessage `json:"caps"`
	}
	if json.Unmarshal(b, &all) != nil || json.Unmarshal(all[host], &e) != nil {
		return nil, false
	}
	return e.Caps, len(e.Caps) > 0
}

const litellmCaps = `{"noninteractive":"declared","tool_use":"none","model_selection":"declared","usage_reporting":"declared","structured_output":"none","cancellation":"none","timeout":"kit","cost_reporting":"none"}`

// Finder locates an external adapter: <project-dir>/adapters.d/<name>.sh. ext:litellm is built in and is used when no such file exists.
type Finder struct{ ProjectDir string }

// Find returns the executable of ext:name ("" and false for the built-in litellm or an unknown name).
func (f Finder) Find(host string) (path string, ok bool) {
	n := strings.TrimPrefix(host, "ext:")
	if n == "" || strings.Trim(n, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-") != "" {
		return "", false
	}
	p := filepath.Join(f.ProjectDir, "adapters.d", n+".sh")
	if st, err := os.Stat(p); err == nil && !st.IsDir() && (runtime.GOOS == "windows" || st.Mode()&0o111 != 0) {
		return p, true
	}
	return "", false
}

// Known reports whether run-role can start host: a built-in, ext:litellm or an executable adapter file.
func (f Finder) Known(host string) bool {
	if Builtin(host) {
		return true
	}
	if _, ok := f.Find(host); ok {
		return true
	}
	return host == "ext:litellm"
}

// ExtCommand is how an adapter file is started: directly, or through sh where the platform cannot run a script.
func ExtCommand(path string) (name string, pre []string) {
	if runtime.GOOS == "windows" {
		return "sh", []string{path}
	}
	return path, nil
}

func (f Finder) extOutput(path string, args ...string) ([]byte, int) {
	name, pre := ExtCommand(path)
	cmd := exec.Command(name, append(pre, args...)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return out.Bytes(), ee.ExitCode()
		}
		return nil, -1
	}
	return out.Bytes(), 0
}

// Capabilities returns the capabilities JSON of host; ok is false when the host is unknown (the message names why).
func (f Finder) Capabilities(host string) (raw []byte, msg string) {
	if strings.HasPrefix(host, "ext:") {
		if p, ok := f.Find(host); ok {
			b, _ := f.extOutput(p, "capabilities")
			return b, ""
		}
		if host == "ext:litellm" {
			return []byte(litellmCaps), ""
		}
		return nil, fmt.Sprintf("adapter: no executable adapter '%s'", strings.TrimPrefix(host, "ext:"))
	}
	if b, ok := builtinCaps(host); ok {
		return b, ""
	}
	return nil, fmt.Sprintf("adapter: unknown host '%s'", host)
}

// Valid reports whether raw has exactly the eight keys with legal values.
func Valid(raw []byte) bool {
	l, err := orderedStrings(raw)
	if err != nil || len(l) != len(Keys) {
		return false
	}
	var got []string
	for _, e := range l {
		got = append(got, e.K)
		if !has(Values, e.V) {
			return false
		}
	}
	want := append([]string(nil), Keys...)
	sort.Strings(got)
	sort.Strings(want)
	return strings.Join(got, ",") == strings.Join(want, ",")
}

func has(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// Run is `sdkit adapter <verb> <host|ext:name> ...`: capabilities, usage, cancel, status, validate.
func (f Finder) Run(args []string, out, errw io.Writer) int {
	verb, host := "", ""
	if len(args) > 0 {
		verb = args[0]
	}
	if len(args) > 1 {
		host = args[1]
	}
	rest := []string{}
	if len(args) > 2 {
		rest = args[2:]
	}
	switch verb {
	case "capabilities", "usage":
		raw, msg := f.Capabilities(host)
		if msg != "" {
			fmt.Fprintln(errw, msg)
			return ExitUsage
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			return ExitInvalid
		}
		l, _ := orderedStrings(raw)
		if verb == "usage" {
			u := "none"
			for _, e := range l {
				if e.K == "usage_reporting" && e.V != "" {
					u = e.V
				}
			}
			if u == "none" {
				fmt.Fprintf(out, "none: %s reports no token usage; its runs are logged as unmeasured, never as zero\n", host)
			} else {
				fmt.Fprintf(out, "%s: %s reports tokens (cache and cost are separate capabilities)\n", u, host)
			}
			return ExitOK
		}
		if len(rest) > 0 && rest[0] == "--json" {
			fmt.Fprintln(out, "{")
			for i, e := range l {
				k, v := jstr(e.K), jstr(e.V)
				c := ","
				if i == len(l)-1 {
					c = ""
				}
				fmt.Fprintf(out, "  %s: %s%s\n", k, v, c)
			}
			fmt.Fprintln(out, "}")
		} else {
			for _, e := range l {
				fmt.Fprintf(out, "%s=%s\n", e.K, e.V)
			}
		}
		return ExitOK
	case "cancel", "status":
		if strings.HasPrefix(host, "ext:") {
			p, ok := f.Find(host)
			if !ok {
				return ExitUsage
			}
			if _, rc := f.extOutput(p, append([]string{verb}, rest...)...); rc == 0 {
				return ExitOK
			}
			fmt.Fprintf(out, "unsupported: %s does not implement %s\n", strings.TrimPrefix(host, "ext:"), verb)
			return ExitUnsupported
		}
		if !Builtin(host) {
			return ExitUsage
		}
		fmt.Fprintf(out, "unsupported: %s has no %s (run-role enforces a timeout; the kit's record is sdd evidence)\n", host, verb)
		return ExitUnsupported
	case "validate":
		raw, msg := f.Capabilities(host)
		if _, isFile := f.Find(host); !strings.HasPrefix(host, "ext:") || msg != "" || (!isFile && host != "ext:litellm") {
			fmt.Fprintf(errw, "adapter: no executable adapter '%s'\n", strings.TrimPrefix(host, "ext:"))
			return ExitUsage
		}
		if !Valid(raw) {
			fmt.Fprintln(out, "invalid: capabilities must be a JSON object with exactly the 8 keys and values in help-verified|declared|estimated|kit|none")
			return ExitInvalid
		}
		fmt.Fprintf(out, "valid: %s\n", strings.TrimPrefix(host, "ext:"))
		return ExitOK
	}
	fmt.Fprintln(errw, "usage: sdkit adapter capabilities|usage|cancel|status|validate <host|ext:name> ...")
	return ExitUsage
}

func jstr(s string) string { b, _ := json.Marshal(s); return string(b) }
