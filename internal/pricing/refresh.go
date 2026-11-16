package pricing

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/store"
)

// DefaultURL is the public AWS Bedrock price list.
const DefaultURL = "https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonBedrockFoundationModels/current/index.json"

// Refresh failures: Refused (exit 3) leaves a hand-made table alone; Failed (exit 4) keeps the old file.
var (
	ErrRefused = errors.New("pricing: refused")
	ErrFailed  = errors.New("pricing: failed")
)

// Options of Refresh. URL and Now default to DefaultURL and the clock.
type Options struct {
	Out      string // table to write
	URL      string
	From     string // a downloaded list instead of fetching (offline, tests)
	Region   string // default us-east-1
	Regional bool   // regional (geo) inference instead of global
	Force    bool
	DryRun   bool
	Now      string
	Client   *http.Client
}

// Refresh rebuilds the table from the live list and returns it. A failed fetch keeps the old file.
func Refresh(o Options) (*Table, error) {
	if o.Region == "" {
		o.Region = "us-east-1"
	}
	if o.URL == "" {
		o.URL = DefaultURL
	}
	if o.Now == "" {
		o.Now = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	}
	if !o.Force {
		if old, err := Load(o.Out); err == nil && !old.Managed() {
			return nil, fmt.Errorf("%w: %s is not a table this tool wrote (no \"managed_by\": \"%s\"): it is yours, so it is left alone. Use --force to replace it, or --out to write elsewhere", ErrRefused, o.Out, Marker)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			if _, e := os.Stat(o.Out); e == nil { // exists but unreadable: not ours either
				return nil, fmt.Errorf("%w: %s is not a table this tool wrote: use --force to replace it", ErrRefused, o.Out)
			}
		}
	}
	raw, err := fetch(o)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFailed, err)
	}
	t, err := fromAWS(raw, o.Region, !o.Regional)
	if err != nil {
		return nil, fmt.Errorf("%w: %v: the table is unchanged", ErrFailed, err)
	}
	t.FetchedAt = o.Now
	if !o.DryRun {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(t); err != nil {
			return nil, err
		}
		if err := store.WriteAtomic(o.Out, b.Bytes()); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrFailed, err)
		}
	}
	return t, nil
}

func fetch(o Options) ([]byte, error) {
	if o.From != "" {
		return os.ReadFile(o.From)
	}
	c := o.Client
	if c == nil {
		c = &http.Client{Timeout: 120 * time.Second}
	}
	resp, err := c.Get(o.URL)
	if err != nil {
		return nil, fmt.Errorf("could not fetch %s: the table is unchanged. Pass --from FILE with a downloaded list, or keep the one you have (%v)", o.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("could not fetch %s: HTTP %d: the table is unchanged", o.URL, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

type awsList struct {
	PublicationDate string `json:"publicationDate"`
	Products        map[string]struct {
		Attributes struct {
			Provider, RegionCode, Model, Usagetype string
		} `json:"attributes"`
	} `json:"products"`
	Terms struct {
		OnDemand map[string]map[string]struct {
			PriceDimensions map[string]struct {
				Unit         string
				PricePerUnit struct{ USD string }
			} `json:"priceDimensions"`
		} `json:"OnDemand"`
	} `json:"terms"`
}

func firstKey[V any](m map[string]V) (string, bool) {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	if len(ks) == 0 {
		return "", false
	}
	return ks[0], true
}

// fromAWS keeps the Anthropic standard-tier on-demand prices of one region; a model needs an input and an output price.
func fromAWS(raw []byte, region string, global bool) (*Table, error) {
	var d awsList
	if err := json.Unmarshal(raw, &d); err != nil || d.Products == nil || d.Terms.OnDemand == nil {
		return nil, errors.New("not an AWS price list (no products/terms)")
	}
	keys := make([]string, 0, len(d.Products))
	for k := range d.Products {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	type price struct{ in, out, cr, cw *float64 }
	by := map[string]*price{}
	for _, k := range keys {
		a := d.Products[k].Attributes
		u := strings.ToLower(a.Usagetype)
		if a.Provider != "Anthropic" || a.RegionCode != region || !strings.HasPrefix(a.Model, "Claude") {
			continue
		}
		if has(u, "batch", "reserved", "1h", "long_ctx", "flex", "priority") || strings.Contains(u, "global") != global {
			continue
		}
		term, ok := d.Terms.OnDemand[k]
		if !ok {
			continue
		}
		tk, _ := firstKey(term)
		dk, ok := firstKey(term[tk].PriceDimensions)
		if !ok {
			continue
		}
		pd := term[tk].PriceDimensions[dk]
		v, err := strconv.ParseFloat(pd.PricePerUnit.USD, 64)
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(pd.Unit), "1k") {
			v *= 1000
		}
		p := by[a.Model]
		if p == nil {
			p = &price{}
			by[a.Model] = p
		}
		switch {
		case has(u, "cacheread", "cache_read"):
			p.cr = &v
		case has(u, "cachewrite", "cache_write"):
			p.cw = &v
		case strings.Contains(u, "input"):
			p.in = &v
		case strings.Contains(u, "output"):
			p.out = &v
		}
	}
	scope := "regional"
	if global {
		scope = "global"
	}
	t := &Table{Version: 2, Currency: "USD", ManagedBy: Marker, PriceListDate: d.PublicationDate,
		Note:   "USD per million tokens, API-equivalent estimate, never a bill. cache_write is the 5-minute rate. Rebuilt by `sdkit doctor --update-pricing` (sdkit pricing refresh); delete managed_by to take the table over by hand.",
		Models: []Rate{}}
	for name, p := range by {
		if p.in == nil || p.out == nil {
			continue
		}
		r := Rate{Model: strings.NewReplacer(" ", "-", ".", "-").Replace(strings.ToLower(name)), Provider: "bedrock", In: *p.in, Out: *p.out,
			Source: fmt.Sprintf("AWS Bedrock price list, %s, %s inference, standard tier", region, scope)}
		if p.cr != nil {
			r.CacheRead = *p.cr
		}
		if p.cw != nil {
			r.CacheWrite = *p.cw
		}
		t.Models = append(t.Models, r)
	}
	if len(t.Models) == 0 {
		return nil, fmt.Errorf("the price list has no Claude model for %s", region)
	}
	sort.Slice(t.Models, func(i, j int) bool { return t.Models[i].Model < t.Models[j].Model })
	return t, nil
}

func has(s string, subs ...string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}
