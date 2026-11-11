package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/hooks"
	"github.com/ashvinbhat/ox/internal/stops"
)

var hookAskCmd = &cobra.Command{
	Use:   "ask",
	Short: "The PreToolUse hook: send a question to Ashvin back unless it's a stop",
	Long: `Claude Code runs this before AskUserQuestion and before a next-step box
(` + "`mcp__ccd_turn__wrap_up`" + `) that asks something. A question that starts with a
kind from ~/.ox/stops.md ("Send: …") passes. Any other is sent back with the
list, so the thread does the work instead of asking. Every question is logged
in ~/.ox/logs/stops.jsonl.

Anything it can't read passes: ox never blocks a thread by failing.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		os.Exit(askHook(os.Stdin, os.Stderr))
	},
}

// askInput is the part of Claude Code's PreToolUse payload the hook reads.
type askInput struct {
	SessionID string `json:"session_id"`
	Tool      string `json:"tool_name"`
	Input     struct {
		Ask       string `json:"ask"`
		Questions []struct {
			Question string `json:"question"`
		} `json:"questions"`
	} `json:"tool_input"`
}

// questions are what the call would ask Ashvin, empty for any other call.
func (in askInput) questions() []string {
	var out []string
	switch in.Tool {
	case "AskUserQuestion":
		for _, q := range in.Input.Questions {
			if strings.TrimSpace(q.Question) != "" {
				out = append(out, q.Question)
			}
		}
	case "mcp__ccd_turn__wrap_up":
		if strings.TrimSpace(in.Input.Ask) != "" {
			out = append(out, in.Input.Ask)
		}
	}
	return out
}

// judgeAsk passes a call only when every question it asks is a stop. kind is
// the first question's.
func judgeAsk(qs []string, list []stops.Stop) (kind string, passed bool) {
	for i, q := range qs {
		k := stops.KindOf(q, list)
		if k == "" {
			return "", false
		}
		if i == 0 {
			kind = k
		}
	}
	return kind, true
}

func sendBack(list []stops.Stop) string {
	var b strings.Builder
	b.WriteString("Sent back: Ashvin is asked only for a stop.\n")
	for _, s := range list {
		fmt.Fprintf(&b, "- %s: %s\n", s.Kind, s.What)
	}
	b.WriteString(`Not a stop? Do it, then say what you did and how to undo it. In the next-step box, leave out the question and its buttons.
A stop? Ask again, starting with its kind, like "Send: post this reply in #backend?".
`)
	return b.String()
}

func askHook(stdin io.Reader, stderr io.Writer) (code int) {
	defer func() {
		if recover() != nil {
			code = 0
		}
	}()
	var in askInput
	if json.NewDecoder(io.LimitReader(stdin, 1<<20)).Decode(&in) != nil {
		return 0
	}
	qs := in.questions()
	if len(qs) == 0 || cfgErr != nil || cfg == nil {
		return 0
	}
	list, _ := stops.Load(cfg.Home)
	kind, passed := judgeAsk(qs, list)
	if os.Getenv(hooks.CheckEnv) != "1" {
		_ = stops.Record(cfg.Home, stops.Entry{At: time.Now(), Session: in.SessionID, Kind: kind,
			Question: strings.Join(qs, "\n"), Passed: passed})
	}
	if passed {
		return 0
	}
	fmt.Fprint(stderr, sendBack(list))
	return 2
}

func init() {
	hookCmd.AddCommand(hookAskCmd)
}
