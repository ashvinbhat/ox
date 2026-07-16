package harness

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ashvinbhat/ox/internal/mission"
	"github.com/ashvinbhat/ox/internal/yokecli"
)

var ticketRe = regexp.MustCompile(`CB-\d+`)

// ResolveTicket finds the external ticket id (CB-XXXXX) for the mission's
// task: task title first, then notes (which also cache Notion lookups), then
// the linked Notion page's "Task ID" unique_id property. Empty when the task
// has no ticket — internal tasks ship without one.
func ResolveTicket(m *mission.Mission) string {
	if m.Yoke == nil {
		return ""
	}
	ref := fmt.Sprintf("%d", m.Yoke.Seq)

	t, err := yokecli.Get(ref)
	if err != nil {
		return ""
	}
	if id := ticketRe.FindString(t.Title); id != "" {
		return id
	}

	notionURL := ""
	if notes, err := yokecli.Notes(ref); err == nil {
		for _, n := range notes {
			if strings.HasPrefix(n.Content, "ticket: ") {
				if id := ticketRe.FindString(n.Content); id != "" {
					return id
				}
			}
		}
	}
	if t.NotionURL != nil && *t.NotionURL != "" {
		notionURL = *t.NotionURL
	}
	if notionURL == "" {
		return ""
	}

	id := notionUniqueID(notionURL)
	if id != "" {
		yokecli.AddNote(ref, "ticket: "+id)
	}
	return id
}

var pageIDRe = regexp.MustCompile(`[0-9a-f]{32}`)

// notionUniqueID reads the page's unique_id property (e.g. "CB-14817") using
// the token yoke already holds. Best-effort: any failure returns "".
func notionUniqueID(pageURL string) string {
	pageID := pageIDRe.FindString(strings.ToLower(pageURL))
	if pageID == "" {
		return ""
	}
	token := yokeEnvValue("NOTION_TOKEN")
	if token == "" {
		return ""
	}

	req, err := http.NewRequest("GET", "https://api.notion.com/v1/pages/"+pageID, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Notion-Version", "2022-06-28")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}

	var page struct {
		Properties map[string]struct {
			Type     string `json:"type"`
			UniqueID *struct {
				Prefix string `json:"prefix"`
				Number int    `json:"number"`
			} `json:"unique_id"`
		} `json:"properties"`
	}
	if json.NewDecoder(resp.Body).Decode(&page) != nil {
		return ""
	}
	for _, prop := range page.Properties {
		if prop.Type == "unique_id" && prop.UniqueID != nil && prop.UniqueID.Prefix != "" {
			return fmt.Sprintf("%s-%d", prop.UniqueID.Prefix, prop.UniqueID.Number)
		}
	}
	return ""
}

func yokeEnvValue(key string) string {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".yoke", ".env"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimPrefix(strings.TrimSpace(line), "export ")
		if v, ok := strings.CutPrefix(line, key+"="); ok {
			return strings.Trim(v, `"'`)
		}
	}
	return ""
}
