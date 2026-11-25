package ext

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/wecii/sdkit/internal/proc"
	"github.com/wecii/sdkit/internal/project"
	"github.com/wecii/sdkit/internal/ui"
)

// Sync states: what happened to the person's copy of the base branch.
const (
	SyncCurrent  = "current"      // nothing new on the server
	SyncUpdated  = "updated"      // new changes were taken in
	SyncOffline  = "offline"      // the server could not be reached; the local copy is used as it is
	SyncDirty    = "dirty"        // there are unsaved edits, so nothing was changed
	SyncOtherBr  = "other-branch" // the folder is on another branch, so the base was not touched
	SyncDiverged = "diverged"     // the local copy has its own commits on the base: only the tech team can sort that out
	SyncNoRemote = "no-remote"
)

// SyncResult is what `sdkit ext sync` prints. Message is for the person, in plain words; State is for the page.
type SyncResult struct {
	State   string `json:"state"`
	Base    string `json:"base"`
	Branch  string `json:"branch"`
	Behind  int    `json:"behind"` // new commits taken (updated) or waiting (skipped)
	Message string `json:"message"`
}

// Sync brings the person's copy up to date with the server, and only ever by fast-forward: it never discards, merges or rewrites
// anything, so a person who knows no git cannot lose work to it. Whatever it cannot do safely it leaves alone and says why.
func Sync(ctx context.Context, root string) SyncResult {
	s, _, _ := project.Load(root)
	base := s.String("BASE_BRANCH")
	if base == "" || strings.HasPrefix(base, "__") {
		base = "main"
	}
	res := SyncResult{Base: base, Branch: ui.Branch(root)}
	env := append([]string{"GIT_TERMINAL_PROMPT=0"}, ui.GitAuthEnv(root)...) // no terminal to ask on
	git := func(timeout time.Duration, args ...string) (proc.Result, error) {
		c, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return proc.Run(c, root, env, "git", args...)
	}

	if r, err := git(10*time.Second, "remote", "get-url", "origin"); err != nil || r.Code != 0 {
		res.State, res.Message = SyncNoRemote, "Bu klasör bir sunucuya bağlı değil; güncelleme yapılamadı."
		return res
	}
	if r, err := git(45*time.Second, "fetch", "-q", "--prune", "origin"); err != nil || r.Code != 0 {
		res.State, res.Message = SyncOffline, "Sunucuya ulaşılamadı; bilgisayarınızdaki kopyayla devam ediliyor."
		return res
	}
	r, err := git(10*time.Second, "rev-list", "--count", "HEAD..origin/"+base)
	if err != nil || r.Code != 0 {
		res.State, res.Message = SyncCurrent, "Sunucuda ana dal bulunamadı; güncelleme yapılmadı."
		return res
	}
	res.Behind, _ = strconv.Atoi(strings.TrimSpace(r.Stdout))
	switch {
	case res.Branch != base:
		// a spec's own branch or a worktree: the base is not what is checked out, and switching branches under the person is not ours to do
		res.State, res.Message = SyncOtherBr, fmt.Sprintf("Şu an %q dalındasınız; ana dal güncellenmedi.", res.Branch)
		return res
	case res.Behind == 0:
		res.State, res.Message = SyncCurrent, "Her şey güncel."
		return res
	}
	if st, err := git(10*time.Second, "status", "--porcelain", "--untracked-files=no"); err != nil || strings.TrimSpace(st.Stdout) != "" {
		res.State, res.Message = SyncDirty, fmt.Sprintf("Kaydedilmemiş değişiklikleriniz var; %d yeni değişiklik beklemede, güncelleme yapılmadı.", res.Behind)
		return res
	}
	if m, err := git(30*time.Second, "merge", "--ff-only", "-q", "origin/"+base); err != nil || m.Code != 0 {
		res.State, res.Message = SyncDiverged, "Bilgisayarınızdaki kopya sunucudan ayrışmış; güncelleme yapılmadı. Teknik ekipten yardım isteyin."
		return res
	}
	res.State, res.Message = SyncUpdated, fmt.Sprintf("%d yeni değişiklik alındı.", res.Behind)
	return res
}

// WriteSync prints Sync's result as one JSON document.
func WriteSync(ctx context.Context, w io.Writer, root string) error {
	return json.NewEncoder(w).Encode(Sync(ctx, root))
}
