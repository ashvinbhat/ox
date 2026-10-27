package journal

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestAppendReadRoundTrip(t *testing.T) {
	home := t.TempDir()
	day := time.Date(2026, 10, 27, 0, 0, 0, 0, time.Local)
	in := []Entry{
		{At: day.Add(9*time.Hour + 5*time.Minute), Kind: Decision, Track: "ox", Thread: "Learning loop · slice",
			Fields: []Field{{"Decided", "markdown day files"}, {"Over", "JSONL"}, {"Why", "he reads it\ndirectly"}}},
		{At: day.Add(15 * time.Hour), Kind: OwnIt, Fields: []Field{{"Own-it", GradePartial}, {"Predicted", "a"}, {"Actual", "b"}, {"Gap", ""}}},
	}
	for _, e := range in {
		if _, err := Append(home, e); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := os.ReadFile(Path(home, day))
	if !strings.HasPrefix(string(raw), "# Journal · Tue 27 Oct 2026\n\n### 09:05 · ox · Learning loop · slice\n") {
		t.Errorf("file starts:\n%s", raw)
	}

	got, err := Read(home, day.AddDate(0, 0, -2), day)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("read %d entries, want 2", len(got))
	}
	d := got[0]
	if d.Kind != Decision || d.Track != "ox" || d.Thread != "Learning loop · slice" || !d.At.Equal(in[0].At) || d.Get("Why") != "he reads it directly" {
		t.Errorf("decision = %+v", d)
	}
	o := got[1]
	if o.Kind != OwnIt || o.Track != "" || o.Thread != "" || o.Get("Own-it") != GradePartial || len(o.Fields) != 3 {
		t.Errorf("own-it = %+v (an empty Gap is left out)", o)
	}
}

func TestSummarize(t *testing.T) {
	now := time.Date(2026, 10, 30, 11, 0, 0, 0, time.Local) // a Friday
	at := func(daysAgo, hour int) time.Time {
		return now.AddDate(0, 0, -daysAgo).Truncate(time.Hour).Add(time.Duration(hour-11) * time.Hour)
	}
	entries := []Entry{
		{At: at(9, 10), Kind: Recap},
		{At: at(8, 10), Kind: Decision}, // outside the 7 days
		{At: at(6, 10), Kind: Decision},
		{At: at(1, 10), Kind: OwnIt, Fields: []Field{{"Own-it", GradeMatch}}},
		{At: at(0, 9), Kind: Decision},
		{At: at(0, 10), Kind: OwnIt, Fields: []Field{{"Own-it", GradeMiss}}},
	}
	s := Summarize(entries, now)
	if s.DecisionsToday != 1 || s.DecisionsWeek != 2 || s.OwnItToday != 1 {
		t.Errorf("counts = %+v", s)
	}
	if s.GradesWeek[GradeMatch] != 1 || s.GradesWeek[GradeMiss] != 1 {
		t.Errorf("grades = %v", s.GradesWeek)
	}
	if !s.LastRecap.Equal(at(9, 10)) || !s.RecapDue {
		t.Errorf("recap = %v due %v", s.LastRecap, s.RecapDue)
	}
}

func TestRecapDue(t *testing.T) {
	fri := time.Date(2026, 10, 30, 0, 0, 0, 0, time.Local)
	lastWeek := fri.AddDate(0, 0, -7).Add(17 * time.Hour)
	cases := []struct {
		name      string
		now, last time.Time
		want      bool
	}{
		{"thursday", fri.Add(-time.Hour), lastWeek, false},
		{"friday, last recap a week ago", fri.Add(9 * time.Hour), lastWeek, true},
		{"friday, done this morning", fri.Add(12 * time.Hour), fri.Add(10 * time.Hour), false},
		{"monday, never done", fri.AddDate(0, 0, 3).Add(9 * time.Hour), time.Time{}, true},
		{"monday, done on saturday", fri.AddDate(0, 0, 3).Add(9 * time.Hour), fri.AddDate(0, 0, 1), false},
		{"tuesday, never done", fri.AddDate(0, 0, 4).Add(9 * time.Hour), time.Time{}, false},
	}
	for _, c := range cases {
		if got := recapDue(c.now, c.last); got != c.want {
			t.Errorf("%s: recapDue = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSummarizeSkipsRecapWithNoDecisions(t *testing.T) {
	now := time.Date(2026, 10, 30, 11, 0, 0, 0, time.Local)
	if s := Summarize(nil, now); s.RecapDue {
		t.Error("a week with no decisions has nothing to recap")
	}
}
