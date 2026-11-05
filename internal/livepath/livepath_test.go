package livepath

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuardPanicsOutsideTheTempDir(t *testing.T) {
	inTemp := filepath.Join(t.TempDir(), ".ox")
	if got := Guard(inTemp); got != inTemp {
		t.Errorf("Guard(%q) = %q", inTemp, got)
	}
	for _, live := range []string{"/Users/someone/.ox", "/home/runner/.claude", filepath.Join(os.TempDir(), "..", "elsewhere")} {
		func() {
			defer func() {
				msg, _ := recover().(string)
				if !strings.Contains(msg, "live path") {
					t.Errorf("Guard(%q) didn't panic with the live path: %q", live, msg)
				}
			}()
			Guard(live)
		}()
	}
}

func TestHomeFollowsHOME(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if got, err := Home(); err != nil || got != tmp {
		t.Errorf("Home() = %q, %v; want %q", got, err, tmp)
	}
}
