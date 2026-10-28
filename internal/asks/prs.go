package asks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CI state of a PR's head commit.
const (
	ChecksPassing = "passing"
	ChecksFailing = "failing"
	ChecksPending = "pending"
	ChecksNone    = "none"
)

// Parked is how long a PR can sit untouched before it stops being current
// work; the queue leaves it out.
const Parked = 14 * 24 * time.Hour

// PR is one of Ashvin's open PRs.
type PR struct {
	Repo      string    `json:"repo"` // owner/name
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	Draft     bool      `json:"draft"`
	Review    string    `json:"review"`    // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED or ""
	Mergeable string    `json:"mergeable"` // MERGEABLE, CONFLICTING or UNKNOWN
	Checks    string    `json:"checks"`
	UpdatedAt time.Time `json:"updatedAt"`
	Mine      bool      `json:"mine"` // in a repo Ashvin owns, where he merges without review
}

// Ref is the short form ox shows: "frontend#9670".
func (p PR) Ref() string {
	repo := p.Repo
	if i := strings.LastIndex(repo, "/"); i >= 0 {
		repo = repo[i+1:]
	}
	return fmt.Sprintf("%s#%d", repo, p.Number)
}

// FetchPRs reads Ashvin's open PRs touched in the last two weeks, with review,
// merge and CI state, from GitHub.
func FetchPRs(ctx context.Context, now time.Time) ([]PR, error) {
	login, err := gh(ctx, "api", "user", "--jq", ".login")
	if err != nil {
		return nil, err
	}
	out, err := gh(ctx, "search", "prs", "--author=@me", "--state=open", "--limit", "50",
		"--json", "repository,number,updatedAt")
	if err != nil {
		return nil, err
	}
	var found []struct {
		Repository struct {
			NameWithOwner string `json:"nameWithOwner"`
		} `json:"repository"`
		Number    int       `json:"number"`
		UpdatedAt time.Time `json:"updatedAt"`
	}
	if err := json.Unmarshal([]byte(out), &found); err != nil {
		return nil, fmt.Errorf("parse gh search: %w", err)
	}

	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		prs  []PR
		errs []error
	)
	for _, f := range found {
		if now.Sub(f.UpdatedAt) > Parked {
			continue
		}
		wg.Add(1)
		go func(repo string, n int) {
			defer wg.Done()
			p, err := fetchPR(ctx, repo, n)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			p.Mine = strings.EqualFold(strings.SplitN(repo, "/", 2)[0], login)
			prs = append(prs, p)
		}(f.Repository.NameWithOwner, f.Number)
	}
	wg.Wait()
	if len(prs) == 0 && len(errs) > 0 {
		return nil, errs[0]
	}
	return prs, nil
}

func fetchPR(ctx context.Context, repo string, n int) (PR, error) {
	out, err := gh(ctx, "pr", "view", fmt.Sprint(n), "-R", repo,
		"--json", "title,url,isDraft,reviewDecision,mergeable,statusCheckRollup,updatedAt")
	if err != nil {
		return PR{}, err
	}
	var v struct {
		Title          string          `json:"title"`
		URL            string          `json:"url"`
		IsDraft        bool            `json:"isDraft"`
		ReviewDecision string          `json:"reviewDecision"`
		Mergeable      string          `json:"mergeable"`
		Checks         json.RawMessage `json:"statusCheckRollup"`
		UpdatedAt      time.Time       `json:"updatedAt"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return PR{}, fmt.Errorf("parse %s#%d: %w", repo, n, err)
	}
	return PR{Repo: repo, Number: n, Title: v.Title, URL: v.URL, Draft: v.IsDraft, Review: v.ReviewDecision,
		Mergeable: v.Mergeable, Checks: checksState(v.Checks), UpdatedAt: v.UpdatedAt}, nil
}

// checksState folds a statusCheckRollup into one state. Cancelled runs are
// superseded by newer ones, so they count as neither passing nor failing.
func checksState(raw json.RawMessage) string {
	var checks []struct {
		Status     string `json:"status"`     // check runs
		Conclusion string `json:"conclusion"` // check runs
		State      string `json:"state"`      // commit statuses
	}
	if json.Unmarshal(raw, &checks) != nil || len(checks) == 0 {
		return ChecksNone
	}
	pending := false
	for _, c := range checks {
		switch {
		case c.Conclusion == "FAILURE" || c.Conclusion == "TIMED_OUT" || c.Conclusion == "STARTUP_FAILURE" ||
			c.Conclusion == "ACTION_REQUIRED" || c.State == "FAILURE" || c.State == "ERROR":
			return ChecksFailing
		case c.State == "PENDING" || c.State == "EXPECTED" || c.Status != "" && c.Status != "COMPLETED":
			pending = true
		}
	}
	if pending {
		return ChecksPending
	}
	return ChecksPassing
}

func gh(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "gh", args...).Output()
	if err != nil {
		return "", fmt.Errorf("gh %s: %w", strings.Join(args[:2], " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Dir holds the PR cache and the drop list: ~/.ox/asks.
func Dir(oxHome string) string { return filepath.Join(oxHome, "asks") }

type prCache struct {
	At  time.Time `json:"at"`
	PRs []PR      `json:"prs"`
}

// LoadPRs reads the cached PR state and when it was fetched.
func LoadPRs(oxHome string) ([]PR, time.Time, error) {
	var c prCache
	err := readJSON(filepath.Join(Dir(oxHome), "prs.json"), &c)
	return c.PRs, c.At, err
}

func SavePRs(oxHome string, prs []PR, at time.Time) error {
	return writeJSON(filepath.Join(Dir(oxHome), "prs.json"), prCache{At: at, PRs: prs})
}

// LoadDropped reads the asks Ashvin dropped, by id, with when.
func LoadDropped(oxHome string) (map[string]time.Time, error) {
	dropped := map[string]time.Time{}
	err := readJSON(filepath.Join(Dir(oxHome), "dropped.json"), &dropped)
	if os.IsNotExist(err) {
		return dropped, nil
	}
	return dropped, err
}

func SaveDropped(oxHome string, dropped map[string]time.Time) error {
	return writeJSON(filepath.Join(Dir(oxHome), "dropped.json"), dropped)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// writeJSON replaces path in one rename, so a brief reading it while the hook
// refreshes never sees half a file.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
