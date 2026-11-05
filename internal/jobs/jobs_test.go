package jobs

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDueWaitsForCadenceAndPacesRetries(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		s    Status
		want bool
	}{
		{"never ran", Status{}, true},
		{"succeeded 30m ago", Status{LastSuccess: now.Add(-30 * time.Minute), LastAttempt: now.Add(-30 * time.Minute)}, false},
		{"due, failed 1m ago", Status{LastSuccess: now.Add(-2 * time.Hour), LastAttempt: now.Add(-time.Minute)}, false},
		{"due, failed 6m ago", Status{LastSuccess: now.Add(-2 * time.Hour), LastAttempt: now.Add(-6 * time.Minute)}, true},
	}
	for _, c := range cases {
		if got := c.s.Due(now, time.Hour); got != c.want {
			t.Errorf("%s: Due = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFailuresCountUntilASuccess(t *testing.T) {
	home := t.TempDir()
	ran, err := Run(home, "prs", time.Hour, time.Minute, func() error { return errors.New("gh: authentication failed\nrun gh auth login") })
	if !ran || err == nil {
		t.Fatalf("first run: ran=%v err=%v, want a failed run", ran, err)
	}
	if s := Load(home, "prs"); s.Failures != 1 || s.Failing() || s.LastError != "gh: authentication failed" {
		t.Fatalf("after one failure: %+v, want 1 failure, not failing, first line kept", s)
	}
	if ran, _ := Run(home, "prs", time.Hour, time.Minute, func() error { return nil }); ran {
		t.Fatal("a retry within RetryAfter ran")
	}
	Note(home, "prs", errors.New("gh: authentication failed"))
	if s := Load(home, "prs"); !s.Failing() {
		t.Fatalf("after two failures: %+v, want failing", s)
	}
	Note(home, "prs", nil)
	if s := Load(home, "prs"); s.Failing() || s.Failures != 0 || s.LastError != "" || s.LastSuccess.IsZero() {
		t.Fatalf("after a success: %+v, want cleared", s)
	}
}

func TestLiveLockHoldsAndStaleLockIsTakenOver(t *testing.T) {
	home := t.TempDir()
	lockPath := filepath.Join(Dir(home), "prune.lock")
	if err := os.MkdirAll(Dir(home), 0o755); err != nil {
		t.Fatal(err)
	}
	ok := func() error { return nil }

	writeLock(t, lockPath, os.Getpid())
	if ran, _ := Run(home, "prune", time.Hour, time.Minute, ok); ran {
		t.Fatal("ran while a live process held a fresh lock")
	}

	old := time.Now().Add(-5 * time.Minute)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}
	if ran, err := Run(home, "prune", time.Hour, time.Minute, ok); !ran || err != nil {
		t.Fatalf("lock older than limit+1m: ran=%v err=%v, want taken over", ran, err)
	}

	writeLock(t, lockPath, deadPid(t))
	if ran, _ := Run(home, "audit", time.Hour, time.Minute, ok); !ran {
		t.Fatal("audit didn't run")
	}
	if err := os.Remove(filepath.Join(Dir(home), "prune.json")); err != nil {
		t.Fatal(err)
	}
	if ran, err := Run(home, "prune", time.Hour, time.Minute, ok); !ran || err != nil {
		t.Fatalf("lock of a dead pid: ran=%v err=%v, want taken over", ran, err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock left behind after the run: %v", err)
	}
}

func TestRunPastLimitIsAFailureAndStops(t *testing.T) {
	home := t.TempDir()
	stopped := false
	defer func(orig func()) { stop = orig }(stop)
	// The real stop kills the process, so nothing deferred runs after it: the
	// lock must be gone and the failure saved before stop is called.
	stop = func() {
		stopped = true
		if _, err := os.Stat(filepath.Join(Dir(home), "prs.lock")); !os.IsNotExist(err) {
			t.Errorf("lock still held when the run was stopped: %v", err)
		}
		if s := Load(home, "prs"); s.Failures != 1 {
			t.Errorf("failure not saved before the run was stopped: %+v", s)
		}
	}

	hang := make(chan struct{})
	defer close(hang)
	ran, err := Run(home, "prs", time.Hour, 20*time.Millisecond, func() error { <-hang; return nil })
	if !ran || err == nil || !strings.Contains(err.Error(), "timed out after 20ms") {
		t.Fatalf("ran=%v err=%v, want a timeout", ran, err)
	}
	if !stopped {
		t.Error("a run past its limit wasn't stopped")
	}
	if s := Load(home, "prs"); s.Failures != 1 || s.LastError != "timed out after 20ms" {
		t.Errorf("status after timeout: %+v", s)
	}
}

func writeLock(t *testing.T, path string, pid int) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func deadPid(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}
