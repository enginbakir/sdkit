// Package route is the deterministic router (F06, policy route-v1): which adapter and model run a role, whether to run, reuse valid
// evidence, retry, escalate or stop. No model is ever called. A decision is a pure function (Decide) of the open task, the settings,
// the registry, the routing ledger and the usage ledger; a risk floor and a mandatory gate are never lowered.
package route

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wecii/sdkit/internal/pricing"
	"github.com/wecii/sdkit/internal/project"
)

// Policy is the version recorded in every decision.
const Policy = "route-v1"

// Rank orders the tiers; 0 is not a tier.
func Rank(t string) int {
	switch t {
	case "light":
		return 1
	case "standard":
		return 2
	case "strong":
		return 3
	}
	return 0
}

func tierOf(r int) string { return [...]string{"", "light", "standard", "strong"}[r] }

var numRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

func isInt(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// Adapter is one entry of core/adapters.json.
type Adapter struct {
	Provider string            `json:"provider"`
	CLI      string            `json:"cli"`
	Caps     map[string]string `json:"caps"`
	Probe    struct {
		Help  []string            `json:"help"`
		Flags map[string][]string `json:"flags"`
	} `json:"probe"`
}

// Keys are the keys of a JSON object in file order.
func Keys(raw []byte) (ks []string) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.Token()
	for d.More() {
		k, _ := d.Token()
		ks = append(ks, k.(string))
		var skip json.RawMessage
		_ = d.Decode(&skip)
	}
	return ks
}

// Adapters keeps the file order (the registry is derived in it).
type Adapters struct {
	Order []string
	M     map[string]Adapter
	Raw   map[string]json.RawMessage
}

// LoadAdapters parses adapters.json text; keys starting with "_" are documentation.
func LoadAdapters(b []byte) (*Adapters, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	a := &Adapters{M: map[string]Adapter{}, Raw: m}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.Token()
	for dec.More() {
		k, _ := dec.Token()
		var raw json.RawMessage
		_ = dec.Decode(&raw)
		if name := k.(string); !strings.HasPrefix(name, "_") {
			var ad Adapter
			if json.Unmarshal(raw, &ad) == nil {
				a.Order, a.M[name] = append(a.Order, name), ad
			}
		}
	}
	return a, nil
}

// Model is one registry entry: models.json entries first, then the ones derived from tiers.env (a model name is never invented).
type Model struct {
	ID              string        `json:"id"`
	Adapter         string        `json:"adapter"`
	Model           *string       `json:"model"`
	Effort          *string       `json:"effort"`
	Tier            string        `json:"tier"`
	Roles           any           `json:"roles"`
	Tools           bool          `json:"tools"`
	Status          string        `json:"status"`
	Source          string        `json:"source"`
	Provider        *string       `json:"provider"`
	Context         any           `json:"context"`
	Cost            *pricing.Rate `json:"cost"`
	UsageAccounting string        `json:"usage_accounting"`
}

func (m Model) model() string {
	if m.Model == nil {
		return ""
	}
	return *m.Model
}

func (m Model) effort() string {
	if m.Effort == nil {
		return ""
	}
	return *m.Effort
}

func (m Model) handles(role string) bool {
	if s, ok := m.Roles.(string); ok {
		return s == "*"
	}
	l, _ := m.Roles.([]any)
	for _, r := range l {
		if r == role {
			return true
		}
	}
	return false
}

func sp(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Registry builds the model registry: <project-dir>/models.json first, then the entries derived from tiers (the KEY=VALUE lines
// of core/tiers.env) and the RUN_ROLE_MODEL_<TIER> settings. models is the project file (nil when absent); err says it is unusable.
func Registry(ad *Adapters, tiers string, get func(string) string, models []byte, prices *pricing.Table) ([]Model, error) {
	env := map[string]string{}
	for _, l := range strings.Split(tiers, "\n") {
		if k, v, ok := strings.Cut(strings.TrimRight(l, "\r"), "="); ok && regexp.MustCompile(`^[A-Z0-9_]+$`).MatchString(k) {
			env[k] = v
		}
	}
	for _, t := range []string{"LIGHT", "STANDARD", "STRONG"} {
		if v := get("RUN_ROLE_MODEL_" + t); v != "" {
			env["RUN_ROLE_MODEL_"+t] = v
		}
	}
	var reg []Model
	if models != nil {
		var raw []json.RawMessage
		if json.Unmarshal(models, &raw) != nil {
			return nil, fmt.Errorf("models.json must be a JSON array")
		}
		for _, r := range raw {
			m := Model{Status: "available", Roles: "*", Tools: true}
			var any1 map[string]any
			_ = json.Unmarshal(r, &any1)
			_ = json.Unmarshal(r, &m)
			if _, ok := any1["id"].(string); !ok {
				m.ID = ""
			}
			m.Source = "models.json"
			reg = append(reg, m)
		}
	}
	have := map[string]bool{}
	for _, m := range reg {
		have[m.ID] = true
	}
	for _, h := range ad.Order {
		var tiers []Model
		for _, t := range []string{"light", "standard", "strong"} {
			up := strings.ToUpper(t)
			m, ok := env["RUN_ROLE_MODEL_"+up]
			if !ok || m == "" {
				m = env[strings.ToUpper(h)+"_MODEL_"+up]
			}
			var eff *string
			if h == "codex" {
				e, ok := env["CODEX_EFFORT_"+up]
				if !ok {
					e = map[string]string{"light": "low", "standard": "medium", "strong": "high"}[t]
				}
				eff = &e
			}
			if m != "" || eff != nil {
				tiers = append(tiers, Model{ID: h + ":" + t, Adapter: h, Model: sp(m), Effort: eff, Tier: t, Roles: "*", Tools: true, Status: "available", Source: "tiers.env"})
			}
		}
		if len(tiers) == 0 {
			tiers = []Model{{ID: h + ":inherit", Adapter: h, Tier: "*", Roles: "*", Tools: true, Status: "available", Source: "inherit"}}
		}
		for _, m := range tiers {
			if !have[m.ID] {
				reg = append(reg, m)
			}
		}
	}
	for i := range reg {
		m := &reg[i]
		if m.Provider == nil {
			m.Provider = sp(ad.M[m.Adapter].Provider)
		}
		if m.Cost == nil && prices != nil {
			if r, ok := prices.Lookup(m.model()); ok {
				m.Cost = &r
			}
		}
		m.UsageAccounting = orDef(ad.M[m.Adapter].Caps["usage_reporting"], "none")
	}
	return reg, nil
}

func orDef(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// RegistryErrors are the problems of the project's models.json entries (the only user input of the registry).
func RegistryErrors(reg []Model, ad *Adapters) []string {
	var out []string
	seen := map[string]int{}
	for _, m := range reg {
		if m.Source != "models.json" {
			continue
		}
		seen[m.ID]++
		if seen[m.ID] == 2 && m.ID != "" {
			out = append(out, "models.json: duplicate id "+m.ID)
		}
	}
	for _, m := range reg {
		if m.Source != "models.json" {
			continue
		}
		p := "models.json: " + m.ID + ": "
		if _, ok := ad.M[m.Adapter]; m.ID == "" {
			out = append(out, "models.json: an entry has no id")
		} else if !ok {
			out = append(out, p+"unknown adapter "+m.Adapter)
		}
		if m.Tier != "*" && Rank(m.Tier) == 0 {
			out = append(out, p+"tier must be light, standard, strong or *")
		}
		if _, l := m.Roles.([]any); m.Roles != "*" && !l {
			out = append(out, p+`roles must be "*" or an array`)
		}
		if m.Status != "available" && m.Status != "disabled" {
			out = append(out, p+"status must be available or disabled")
		}
	}
	return out
}

// Available finds an available registry entry by id.
func Available(reg []Model, id string) (Model, bool) {
	for _, m := range reg {
		if m.ID == id && m.Status == "available" {
			return m, true
		}
	}
	return Model{}, false
}

// KitInput is an Input with the kit's adapters, the registry derived from core/tiers.env and the settings (plus models, the project's
// models.json, when not nil), the kit's role texts and the process's CLIs. The caller fills in the request and the task's facts.
func KitInput(get func(string) string, models []byte, pt *pricing.Table) Input {
	in := Input{Get: get, HasTimeout: true, HasCLI: func(c string) bool { _, err := exec.LookPath(c); return err == nil },
		RoleText: func(b string) (string, bool) {
			t, err := project.KitFile("core/roles/" + b + ".md")
			return string(t), err == nil
		}}
	ab, _ := project.KitFile("core/adapters.json")
	var err error
	if in.Adapters, err = LoadAdapters(ab); err != nil {
		in.AdaptersErr, in.Adapters = true, &Adapters{M: map[string]Adapter{}}
	}
	tb, _ := project.KitFile("core/tiers.env")
	if in.Reg, err = Registry(in.Adapters, string(tb), get, models, pt); err != nil {
		in.RegErr = err.Error()
	}
	return in
}

// PricesFile is the user's price table (TELEMETRY_PRICES, else <project-dir>/pricing.json).
func PricesFile(p project.Project, get func(string) string) string {
	if f := get("TELEMETRY_PRICES"); f != "" {
		return f
	}
	return filepath.Join(p.Dir(), "pricing.json")
}

// KitAdapters is the embedded core/adapters.json.
func KitAdapters() ([]byte, error) { return project.KitFile("core/adapters.json") }

// KitAdapters is the embedded core/adapters.json.
