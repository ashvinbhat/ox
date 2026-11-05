// Package jobs runs ox's background jobs (worktree prune, memory audit, PR
// refresh) at most once per interval and remembers how each run went, so a
// job that keeps failing shows up in the brief instead of going quiet.
package jobs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// RetryAfter paces runs after a failure. A failed hourly job retries in
// minutes, not an hour later.
const RetryAfter = 5 * time.Minute

// FailingAfter is how many failures in a row make a job a problem worth a
// line in the brief. One failure is often a blip, like a dropped network.
const FailingAfter = 2

// Status is how a job's last runs went.
type Status struct {
	Name        string    `json:"name"`
	LastAttempt time.Time `json:"lastAttempt"`
	LastSuccess time.Time `json:"lastSuccess"`
	LastError   string    `json:"lastError,omitempty"`
	Failures    int       `json:"failures,omitempty"`
}

// Dir holds one status file and one lock file per job.
func Dir(oxHome string) string { return filepath.Join(oxHome, "jobs") }

// Load reads a job's status; a job that never ran has a zero Status.
func Load(oxHome, name string) Status {
	s := Status{Name: name}
	b, err := os.ReadFile(filepath.Join(Dir(oxHome), name+".json"))
	if err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func save(oxHome string, s Status) error {
	if err := os.MkdirAll(Dir(oxHome), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(Dir(oxHome), s.Name+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Due reports whether a job should run: its last success is older than
// every, and it hasn't tried in the last RetryAfter.
func (s Status) Due(now time.Time, every time.Duration) bool {
	return now.Sub(s.LastSuccess) >= every && now.Sub(s.LastAttempt) >= RetryAfter
}

// Failing reports a job that failed FailingAfter times in a row.
func (s Status) Failing() bool { return s.Failures >= FailingAfter }

// Run runs fn when the job is due and no other run of it is live. It records
// the attempt before fn starts and the outcome after. A run past limit is
// recorded as failed and its process group is killed, so a hung gh call
// can't hold the job forever.
func Run(oxHome, name string, every, limit time.Duration, fn func() error) (ran bool, err error) {
	now := time.Now()
	if !Load(oxHome, name).Due(now, every) {
		return false, nil
	}
	release, ok := lock(oxHome, name, limit, now)
	if !ok {
		return false, nil
	}
	defer release()
	// Another run may have finished between the check and the lock.
	s := Load(oxHome, name)
	if !s.Due(now, every) {
		return false, nil
	}
	s.LastAttempt = now
	if err := save(oxHome, s); err != nil {
		return false, err
	}

	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err = <-done:
	case <-time.After(limit):
		err = fmt.Errorf("timed out after %s", limit)
		record(oxHome, s, err)
		release()
		stop()
		return true, err
	}
	record(oxHome, s, err)
	return true, err
}

// Note records the outcome of a run made outside Run, such as `ox asks
// refresh` typed by hand, so a success clears a failing job.
func Note(oxHome, name string, err error) {
	s := Load(oxHome, name)
	s.LastAttempt = time.Now()
	record(oxHome, s, err)
}

func record(oxHome string, s Status, err error) {
	if err != nil {
		s.Failures++
		s.LastError = firstLine(err.Error())
	} else {
		s.Failures, s.LastError, s.LastSuccess = 0, "", time.Now()
	}
	_ = save(oxHome, s)
}

// stop ends a run that outlived its limit. The hook starts each job as its
// own process group, so killing the group takes its gh children too; a job
// run from a terminal only exits, leaving the shell's group alone.
var stop = func() {
	if syscall.Getpgrp() == os.Getpid() {
		_ = syscall.Kill(-os.Getpid(), syscall.SIGKILL)
	}
	os.Exit(1)
}

// lock takes the job's lock file, which names the holder's pid. A lock whose
// holder is dead, or that is older than the job's limit, is stale and taken
// over: a crashed run never blocks the next.
func lock(oxHome, name string, limit time.Duration, now time.Time) (release func(), ok bool) {
	if err := os.MkdirAll(Dir(oxHome), 0o755); err != nil {
		return nil, false
	}
	path := filepath.Join(Dir(oxHome), name+".lock")
	for range 2 {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			f.Close()
			var once bool
			return func() {
				if !once {
					once = true
					os.Remove(path)
				}
			}, true
		}
		if !errors.Is(err, os.ErrExist) || !stale(path, limit, now) {
			return nil, false
		}
		os.Remove(path)
	}
	return nil, false
}

func stale(path string, limit time.Duration, now time.Time) bool {
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	if now.Sub(info.ModTime()) > limit+time.Minute {
		return true
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return true
	}
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}
