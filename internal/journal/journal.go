// Package journal is the decision journal: one markdown file a day under
// ~/.ox/journal, where threads log the choices they make, own-it rounds and
// weekly recaps, so Ashvin can read why the work went the way it did.
package journal

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Kind is what an entry records; its first field names it.
type Kind string

const (
	Decision Kind = "decision"
	OwnIt    Kind = "own-it"
	Recap    Kind = "recap"
)

// Own-it grades: how close Ashvin's predicted approach came to the one built.
const (
	GradeMatch   = "match"
	GradePartial = "partial"
	GradeMiss    = "miss"
)

// OwnItPerDay caps the own-it rounds threads offer on their own; more than
// that turns practice into a toll on every task.
const OwnItPerDay = 2

// firstKey maps an entry's leading field to its kind.
var firstKey = map[string]Kind{"Decided": Decision, "Own-it": OwnIt, "Recap": Recap}

// Field is one "- Key: value" line.
type Field struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Entry is one journal block.
type Entry struct {
	At     time.Time `json:"at"`
	Kind   Kind      `json:"kind"`
	Track  string    `json:"track,omitempty"`
	Thread string    `json:"thread,omitempty"`
	Fields []Field   `json:"fields"`
}

// Get returns the value of a field, or "".
func (e Entry) Get(key string) string {
	for _, f := range e.Fields {
		if f.Key == key {
			return f.Value
		}
	}
	return ""
}

// Dir is ~/.ox/journal.
func Dir(oxHome string) string { return filepath.Join(oxHome, "journal") }

// Path is the day file holding entries made on t's local date.
func Path(oxHome string, t time.Time) string {
	return filepath.Join(Dir(oxHome), t.Format("2006-01-02")+".md")
}

// Format renders an entry as the markdown block Append writes.
func Format(e Entry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### %s · %s · %s\n", e.At.Format("15:04"), orDash(e.Track), orDash(oneLine(e.Thread)))
	for _, f := range e.Fields {
		if f.Value != "" {
			fmt.Fprintf(&b, "- %s: %s\n", f.Key, oneLine(f.Value))
		}
	}
	return b.String()
}

// Append adds an entry to its day file, creating the file with a title.
func Append(oxHome string, e Entry) (string, error) {
	path := Path(oxHome, e.At)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() == 0 {
		fmt.Fprintf(f, "# Journal · %s\n", e.At.Format("Mon 2 Jan 2006"))
	}
	_, err = f.WriteString("\n" + Format(e))
	return path, err
}

var (
	headerRe = regexp.MustCompile(`^### (\d\d:\d\d) · (.*?) · (.*)$`)
	fieldRe  = regexp.MustCompile(`^- ([A-Za-z-]+): (.*)$`)
)

// Read returns the entries of every day from `from` through `to`, oldest
// first. A day without a file has no entries.
func Read(oxHome string, from, to time.Time) ([]Entry, error) {
	var out []Entry
	start := dayStart(from)
	days := int(dayStart(to).Sub(start).Hours()/24+0.5) + 1
	for i := range days {
		day := start.AddDate(0, 0, i)
		entries, err := readDay(Path(oxHome, day), day)
		if err != nil {
			return nil, err
		}
		out = append(out, entries...)
	}
	return out, nil
}

func readDay(path string, day time.Time) ([]Entry, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if m := headerRe.FindStringSubmatch(line); m != nil {
			clock, _ := time.ParseInLocation("15:04", m[1], day.Location())
			at := day.Add(time.Duration(clock.Hour())*time.Hour + time.Duration(clock.Minute())*time.Minute)
			out = append(out, Entry{At: at, Track: undash(m[2]), Thread: undash(m[3])})
			continue
		}
		if m := fieldRe.FindStringSubmatch(line); m != nil && len(out) > 0 {
			e := &out[len(out)-1]
			if len(e.Fields) == 0 {
				e.Kind = firstKey[m[1]]
			}
			e.Fields = append(e.Fields, Field{Key: m[1], Value: m[2]})
		}
	}
	return out, sc.Err()
}

// Status sums up the journal for the brief and the skills.
type Status struct {
	DecisionsToday int            `json:"decisionsToday"`
	OwnItToday     int            `json:"ownItToday"`
	DecisionsWeek  int            `json:"decisionsWeek"` // last 7 days, today included
	GradesWeek     map[string]int `json:"gradesWeek,omitempty"`
	LastRecap      time.Time      `json:"lastRecap,omitzero"`
	RecapDue       bool           `json:"recapDue"`
}

// Summarize reads the last 7 days (and the last recap, which may be older)
// as of now.
func Summarize(entries []Entry, now time.Time) Status {
	today := dayStart(now)
	weekStart := today.AddDate(0, 0, -6)
	s := Status{GradesWeek: map[string]int{}}
	for _, e := range entries {
		if e.Kind == Recap && e.At.After(s.LastRecap) {
			s.LastRecap = e.At
		}
		if e.At.Before(weekStart) {
			continue
		}
		switch e.Kind {
		case Decision:
			s.DecisionsWeek++
			if !e.At.Before(today) {
				s.DecisionsToday++
			}
		case OwnIt:
			s.GradesWeek[e.Get("Own-it")]++
			if !e.At.Before(today) {
				s.OwnItToday++
			}
		}
	}
	s.RecapDue = recapDue(now, s.LastRecap) && s.DecisionsWeek > 0
	return s
}

// recapDue is true from Friday through Monday until a recap is done after
// that Friday began. A recap on Monday covers the week just ended.
func recapDue(now, lastRecap time.Time) bool {
	sinceFriday := (int(now.Weekday()) - int(time.Friday) + 7) % 7
	if sinceFriday > 3 {
		return false
	}
	friday := dayStart(now).AddDate(0, 0, -sinceFriday)
	return lastRecap.Before(friday)
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func undash(s string) string {
	if s == "-" {
		return ""
	}
	return s
}
