package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/proc"
	"github.com/wecii/sdkit/internal/setup"
)

// Health levels. A problem stops the work, a todo is something the person can fix, a note is shown only under the details.
const (
	LevelOK      = "ok"
	LevelProblem = "problem"
	LevelTodo    = "todo"
	LevelNote    = "note"
)

// Fix names the screen that solves an item; empty means the person cannot fix it themselves and should tell the tech team.
const FixConnect = "connect"

// The other fixes are things a person can start themselves where the page that shows the item can run a command (the editor
// extension does; the web panel only offers FixConnect).
const (
	FixInstallOpenSpec = "install-openspec"
	FixInstallClaude   = "install-claude"
	FixLoginClaude     = "login-claude"
)

// Item is one line of the Health screen.
type Item struct {
	Level string `json:"level"`
	Title string `json:"title"`          // what is wrong or fine, in plain words
	Help  string `json:"help,omitempty"` // what to do
	Fix   string `json:"fix,omitempty"`
	// Check names what was checked (claude, claude-login, openspec, jira, bitbucket, project, setup), so a page can tell which
	// check failed without reading the words of the title. A check fails when an item with its name is a problem or a todo.
	Check string `json:"check,omitempty"`
	Raw   string `json:"raw,omitempty"` // the original `sdkit doctor` line, under "details"
	// Soft marks a problem the person cannot fix and that does not stop the work (the Tech Lead's to do); Ready ignores it.
	Soft bool `json:"soft,omitempty"`
	// Main is true for what the person should see first: every problem and todo, and the few good news that matter to them.
	Main bool `json:"main,omitempty"`
}

// goodNews are the passing checks worth a line of their own; the other dozens of `ok` lines stay under the details.
var goodNews = []struct {
	re    *regexp.Regexp
	title string
	check string
}{
	{regexp.MustCompile(`Jira reachable and the credentials work`), "Jira bağlantısı çalışıyor", "jira"},
	{regexp.MustCompile(`openspec CLI installed`), "OpenSpec kurulu", "openspec"},
}

// rule maps a doctor line to plain words. The first rule that matches wins; a rule with an empty Title hides the line from the
// analyst (developer topics such as the CI gate), and it stays reachable under the details.
type rule struct {
	re    *regexp.Regexp
	title string
	help  string
	fix   string
	level string // forces a level; empty keeps the one doctor gave
	soft  bool
	check string // see Item.Check
}

var rules = []rule{
	{regexp.MustCompile(`server-side gate|scaffold-version|scaffold-shared|drift|upgrade`), "", "", "", LevelNote, false, ""},
	// the network being down is not something the person can fix on the Connect page, and spec writing does not need Jira
	{regexp.MustCompile(`Jira (is )?not reachable|Jira answered`), "Jira'ya şu an ulaşılamıyor", "İnternet bağlantınızı kontrol edin. Spec yazmayı etkilemez, görev üretmek için gerekir.", "", LevelProblem, true, "jira"},
	{regexp.MustCompile(`JIRA_|TRACKER is not jira|Jira refused`), "Jira bağlantısı eksik veya çalışmıyor", "Configuration sayfasında Jira bilgilerini girin.", FixConnect, LevelTodo, false, "jira"},
	{regexp.MustCompile(`(?i)bitbucket (api )?token|bitbucket account|BITBUCKET_|Bitbucket (rejected|not reachable)`), "Bitbucket bağlantısı eksik veya çalışmıyor", "Configuration sayfasında Bitbucket bilgilerini girin.", FixConnect, LevelTodo, false, "bitbucket"},
	{regexp.MustCompile(`project\.md still has TODO`), "Proje profili (project.md) doldurulmamış", "Bunu Tech Lead'in doldurması gerekir.", "", LevelProblem, true, "project"},
	{regexp.MustCompile(`openspec CLI`), "OpenSpec kurulu değil", "Spec üretimi için gerekli. Teknik ekipten kurmasını isteyin.", FixInstallOpenSpec, LevelProblem, false, "openspec"},
	{regexp.MustCompile(`not a git repository`), "Bu klasör bir git deposu değil", "sdkit'i deponun içinden başlatın.", "", LevelProblem, false, "setup"},
	{regexp.MustCompile(`settings\.local\.json .*missing`), "Bu bilgisayarda kurulum tamamlanmamış", "Teknik ekipten bu depoda `sdkit init` çalıştırmasını isteyin.", "", LevelProblem, false, "setup"},
}

var doctorLine = regexp.MustCompile(`^\s*(ok|warn|FAIL|TODO\s+\d+\.)\s+(.*)$`)

// ParseDoctor turns the text of `sdkit doctor` into items, one per distinct plain title (five missing Jira settings are one item,
// not five). Lines that are not a check (headings, hints) are dropped.
func ParseDoctor(out string) []Item {
	var items []Item
	index := map[string]int{}
	for _, l := range strings.Split(out, "\n") {
		m := doctorLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		level, text := map[string]string{"ok": LevelOK, "warn": LevelNote, "FAIL": LevelProblem}[m[1]], strings.TrimSpace(m[2])
		if strings.HasPrefix(m[1], "TODO") {
			level = LevelTodo
		}
		it := Item{Level: level, Title: text, Raw: strings.TrimSpace(l)}
		if level == LevelOK {
			for _, g := range goodNews {
				if g.re.MatchString(text) {
					it.Title, it.Main, it.Check = g.title, true, g.check
				}
			}
		}
		for _, r := range rules {
			if r.re.MatchString(text) && level != LevelOK {
				it.Title, it.Help, it.Fix, it.Soft, it.Check = r.title, r.help, r.fix, r.soft, r.check
				if r.level != "" {
					it.Level = r.level
				}
				break
			}
		}
		if it.Level == LevelNote || it.Title == "" {
			it.Level, it.Title = LevelNote, text
		}
		if it.Level == LevelProblem || it.Level == LevelTodo {
			it.Main = true
		}
		if i, seen := index[it.Title]; seen && it.Level != LevelOK {
			items[i].Raw += "\n" + it.Raw
			continue
		}
		index[it.Title] = len(items)
		items = append(items, it)
	}
	return items
}

// Ready is true when nothing the person or the team must fix is left.
func Ready(items []Item) bool {
	for _, it := range items {
		if (it.Level == LevelProblem || it.Level == LevelTodo) && !it.Soft {
			return false
		}
	}
	return true
}

// Health runs the checks of `sdkit doctor` on root and adds the two a person cares about first: Claude Code installed and logged in.
func Health(ctx context.Context, root string, getenv func(string) string, host string) []Item {
	var buf bytes.Buffer
	s := setup.New(root)
	s.Out = &buf
	setup.Doctor(root, s, getenv)
	items := append(hostItem(ctx, host), ParseDoctor(buf.String())...)
	// what needs doing first, then what is fine, then the notes; each group keeps doctor's order
	rank := func(l string) int { return map[string]int{LevelProblem: 0, LevelTodo: 0, LevelOK: 1}[l] }
	sort.SliceStable(items, func(i, j int) bool { return rank(items[i].Level) < rank(items[j].Level) })
	return items
}

// hostItem checks the agent host the panel drives. Only Claude Code has a login check; another host is reported as present or not.
func hostItem(ctx context.Context, host string) []Item {
	if host == "" {
		host = "claude"
	}
	if _, err := proc.LookPath(host); err != nil {
		return []Item{{Level: LevelProblem, Main: true, Check: "claude", Title: "Claude Code kurulu değil", Help: "Spec üretimi Claude Code ile çalışır. Teknik ekipten kurmasını isteyin.", Fix: FixInstallClaude}}
	}
	if host != "claude" {
		return []Item{{Level: LevelOK, Title: host + " kurulu", Main: true}}
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, host, "auth", "status").Output()
	var st struct {
		LoggedIn bool `json:"loggedIn"`
	}
	if err != nil || json.Unmarshal(out, &st) != nil || !st.LoggedIn {
		return []Item{{Level: LevelProblem, Main: true, Check: "claude-login", Title: "Claude Code'a giriş yapılmamış", Help: "Teknik ekipten bu bilgisayarda Claude Code'a giriş yapmasını isteyin.", Fix: FixLoginClaude}}
	}
	return []Item{{Level: LevelOK, Title: "Claude Code hazır", Main: true, Check: "claude-login"}}
}
