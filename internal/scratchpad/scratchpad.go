package scratchpad

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Entry represents a single scratchpad entry.
type Entry struct {
	AgentID   string
	Kind      string // discovery, question, decision, blocker
	Content   string
	Timestamp time.Time
}

// Scratchpad manages a shared append-only markdown file for agent communication.
type Scratchpad struct {
	path string
}

// New creates a new scratchpad manager.
func New(agentsDir string) *Scratchpad {
	return &Scratchpad{
		path: agentsDir + "/scratchpad.md",
	}
}

// Append adds a new entry to the scratchpad.
func (s *Scratchpad) Append(entry Entry) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	// Create file with header if it doesn't exist
	if _, err := os.Stat(s.path); os.IsNotExist(err) {
		header := "# Scratchpad\n\nShared notes between agents.\n\n---\n\n"
		if err := os.WriteFile(s.path, []byte(header), 0o644); err != nil {
			return fmt.Errorf("create scratchpad: %w", err)
		}
	}

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open scratchpad: %w", err)
	}
	defer f.Close()

	line := fmt.Sprintf("### [%s] %s (%s)\n%s\n\n",
		entry.Timestamp.Format("15:04"),
		entry.AgentID,
		entry.Kind,
		entry.Content,
	)

	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("write entry: %w", err)
	}

	return nil
}

// Read returns the full scratchpad content.
func (s *Scratchpad) Read() (string, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return "(empty scratchpad)", nil
		}
		return "", fmt.Errorf("read scratchpad: %w", err)
	}
	return string(data), nil
}

// ReadSince returns entries written after the given time.
func (s *Scratchpad) ReadSince(since time.Time) (string, error) {
	content, err := s.Read()
	if err != nil {
		return "", err
	}

	// Parse entries and filter by time
	var result strings.Builder
	lines := strings.Split(content, "\n")
	include := false

	for _, line := range lines {
		// Check if this is a timestamp header line
		if strings.HasPrefix(line, "### [") {
			// Extract time from header
			if t, ok := parseEntryTime(line); ok {
				include = t.After(since) || t.Equal(since)
			}
		}
		if include {
			result.WriteString(line)
			result.WriteString("\n")
		}
	}

	if result.Len() == 0 {
		return "(no entries since " + since.Format("15:04") + ")", nil
	}

	return result.String(), nil
}

// Path returns the scratchpad file path.
func (s *Scratchpad) Path() string {
	return s.path
}

func parseEntryTime(line string) (time.Time, bool) {
	// Format: ### [15:04] agent-id (kind)
	if !strings.HasPrefix(line, "### [") {
		return time.Time{}, false
	}
	end := strings.Index(line, "]")
	if end < 6 {
		return time.Time{}, false
	}
	timeStr := line[5:end]
	now := time.Now()
	t, err := time.Parse("15:04", timeStr)
	if err != nil {
		return time.Time{}, false
	}
	// Set date to today
	t = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
	return t, true
}
