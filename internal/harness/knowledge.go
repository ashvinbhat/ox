package harness

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/memory"
	"github.com/ashvinbhat/ox/internal/memory/embed"
	"github.com/ashvinbhat/ox/internal/mission"
)

// PriorKnowledge renders the "Prior knowledge" brief section: top-k relevant
// memories for the mission's repos/task, budgeted small. Empty string when
// nothing relevant exists — never an error, memory must not block spawning.
func PriorKnowledge(cfg *config.Config, m *mission.Mission, query string, k int) string {
	store, err := memory.Open(cfg.Home, embed.New(cfg.Memory.Embeddings))
	if err != nil {
		return ""
	}
	defer store.Close()

	var scopes []string
	for repo := range m.Repos {
		scopes = append(scopes, "repo:"+repo)
	}
	if m.Yoke != nil {
		scopes = append(scopes, fmt.Sprintf("task:%d", m.Yoke.Seq))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if k <= 0 {
		k = 6
	}
	mems, _, err := store.Search(ctx, query, memory.SearchOptions{Scopes: scopes, K: k})
	if err != nil || len(mems) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Prior knowledge (long-term memory — cite or supersede via remember)\n")
	for _, mem := range mems {
		fmt.Fprintf(&sb, "- [%s] %s (mem %s)\n", mem.Kind, mem.Content, mem.UID)
	}
	return sb.String()
}
