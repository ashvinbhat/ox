package mission

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Event is one line of events.jsonl. N is monotonic per mission so consumers
// (watcher digests, mission_status "unread events") can keep cursors.
type Event struct {
	N     int64          `json:"n"`
	TS    time.Time      `json:"ts"`
	Type  string         `json:"type"`
	Actor string         `json:"actor"`
	Data  map[string]any `json:"data,omitempty"`
}

func (m *Mission) eventsPath() string { return filepath.Join(m.dir, "events.jsonl") }

// AppendEvent writes one event line. The whole read-tail+append runs under
// the mission lock so N stays monotonic across processes.
func (m *Mission) AppendEvent(typ, actor string, data map[string]any) error {
	return withFileLock(filepath.Join(m.dir, ".lock"), func() error {
		last, _ := lastEventN(m.eventsPath())
		ev := Event{N: last + 1, TS: time.Now(), Type: typ, Actor: actor, Data: data}
		line, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("marshal event: %w", err)
		}
		f, err := os.OpenFile(m.eventsPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("open events: %w", err)
		}
		defer f.Close()
		_, err = f.Write(append(line, '\n'))
		return err
	})
}

// EventsSince returns events with N > after.
func (m *Mission) EventsSince(after int64) ([]Event, error) {
	f, err := os.Open(m.eventsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var events []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var ev Event
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.N > after {
			events = append(events, ev)
		}
	}
	return events, sc.Err()
}

func lastEventN(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, nil
	}
	defer f.Close()

	var last int64
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var ev Event
		if json.Unmarshal(sc.Bytes(), &ev) == nil && ev.N > last {
			last = ev.N
		}
	}
	return last, nil
}
