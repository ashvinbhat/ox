// Package costs prices interactive claude sessions by tailing their
// transcripts. Sessions get pre-assigned IDs at launch, so the transcript
// path is deterministic: ~/.claude/projects/<munged-cwd>/<session-id>.jsonl.
// Headless jobs don't need this — their result JSON carries exact cost.
package costs

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Price is USD per million tokens.
type Price struct {
	Input      float64
	Output     float64
	CacheWrite float64 // per MTok written to cache
	CacheRead  float64 // per MTok read from cache
}

// prices as of mid-2026; override via ox.yaml when they drift.
var priceTable = map[string]Price{
	"opus":   {Input: 5, Output: 25, CacheWrite: 6.25, CacheRead: 0.5},
	"sonnet": {Input: 3, Output: 15, CacheWrite: 3.75, CacheRead: 0.3},
	"haiku":  {Input: 1, Output: 5, CacheWrite: 1.25, CacheRead: 0.1},
}

// PriceFor resolves a model name or full ID to its tier price.
func PriceFor(model string) Price {
	m := strings.ToLower(model)
	for tier, p := range priceTable {
		if strings.Contains(m, tier) {
			return p
		}
	}
	return priceTable["sonnet"]
}

type Usage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheWriteTokens int64
	CacheReadTokens  int64
}

func (u Usage) Add(o Usage) Usage {
	return Usage{
		InputTokens:      u.InputTokens + o.InputTokens,
		OutputTokens:     u.OutputTokens + o.OutputTokens,
		CacheWriteTokens: u.CacheWriteTokens + o.CacheWriteTokens,
		CacheReadTokens:  u.CacheReadTokens + o.CacheReadTokens,
	}
}

func (u Usage) CostUSD(model string) float64 {
	p := PriceFor(model)
	return float64(u.InputTokens)/1e6*p.Input +
		float64(u.OutputTokens)/1e6*p.Output +
		float64(u.CacheWriteTokens)/1e6*p.CacheWrite +
		float64(u.CacheReadTokens)/1e6*p.CacheRead
}

// TranscriptPath returns the claude transcript file for a session launched in
// cwd. Claude Code munges the cwd by replacing '/' and '.' with '-'.
func TranscriptPath(cwd, sessionID string) string {
	home, _ := os.UserHomeDir()
	munged := strings.NewReplacer("/", "-", ".", "-").Replace(cwd)
	return filepath.Join(home, ".claude", "projects", munged, sessionID+".jsonl")
}

type transcriptLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens         int64 `json:"input_tokens"`
			OutputTokens        int64 `json:"output_tokens"`
			CacheCreationTokens int64 `json:"cache_creation_input_tokens"`
			CacheReadTokens     int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// Tail parses transcript lines past offset. Returns the usage delta, an
// estimate of the current context size (last assistant turn's total input),
// the time of the last user message seen in the delta (zero when none), and
// the new offset to persist.
func Tail(path string, offset int64) (delta Usage, contextTokens int64, lastUserAt time.Time, newOffset int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return Usage{}, 0, time.Time{}, offset, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return Usage{}, 0, time.Time{}, offset, err
	}
	// Truncated/rotated file: start over.
	if offset > info.Size() {
		offset = 0
	}
	if _, err := f.Seek(offset, 0); err != nil {
		return Usage{}, 0, time.Time{}, offset, err
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256*1024), 8*1024*1024)
	newOffset = offset
	for sc.Scan() {
		raw := sc.Bytes()
		newOffset += int64(len(raw)) + 1
		var line transcriptLine
		if json.Unmarshal(raw, &line) != nil {
			continue
		}
		switch line.Type {
		case "user":
			if t, terr := time.Parse(time.RFC3339, line.Timestamp); terr == nil {
				lastUserAt = t
			} else {
				lastUserAt = time.Now()
			}
		case "assistant":
			u := line.Message.Usage
			delta = delta.Add(Usage{
				InputTokens:      u.InputTokens,
				OutputTokens:     u.OutputTokens,
				CacheWriteTokens: u.CacheCreationTokens,
				CacheReadTokens:  u.CacheReadTokens,
			})
			contextTokens = u.InputTokens + u.CacheCreationTokens + u.CacheReadTokens
		}
	}
	return delta, contextTokens, lastUserAt, newOffset, sc.Err()
}
