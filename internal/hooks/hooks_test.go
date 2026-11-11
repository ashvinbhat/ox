package hooks

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInitRetiresOldBuiltInsAndKeepsCustomHooks(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "hooks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ox-brief.sh", "ox-instructions.sh", "yoke-ready-tasks.sh", "workspace-context.sh", "my-own.sh"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/bash\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	m := NewManager(home)
	if names := hookNames(m); !reflect.DeepEqual(names, []string{"ox-brief", "my-own"}) {
		t.Fatalf("hooks before Init = %v, want [ox-brief my-own]: a retired script is never listed", names)
	}
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	left, _ := os.ReadDir(dir)
	if len(left) != 1 || left[0].Name() != "my-own.sh" {
		t.Fatalf("hooks dir after Init = %v, want only my-own.sh", left)
	}
	if h, _ := m.Get("ox-brief"); h == nil || !h.BuiltIn || h.Script != Command {
		t.Fatalf("ox-brief = %+v, want the built-in command", h)
	}
}

const theirs = `{
  "model": "opus",
  "hooks": {
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "guard.sh"}]}],
    "SessionStart": [
      {"matcher": "", "hooks": [
        {"type": "command", "command": "/Users/me/bin/their-own.sh"},
        {"type": "command", "command": "/Users/me/.ox/hooks/ox-brief.sh"}
      ]},
      {"matcher": "startup", "hooks": [{"type": "command", "command": "/Users/me/.ox/hooks/yoke-ready-tasks.sh"}]}
    ]
  }
}
`

func TestInstallTurnsOnBoardReloadAndKeepsTheirValue(t *testing.T) {
	cases := []struct{ name, settings, want, foo string }{
		{"no env", theirs, "1", ""},
		{"env without it", `{"env": {"FOO": "bar"}}`, "1", "bar"},
		{"their value", `{"env": {"FOO": "bar", "` + WatchEnv + `": "0"}}`, "0", "bar"},
	}
	m := NewManager(t.TempDir())
	for _, c := range cases {
		path := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(path, []byte(c.settings), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := m.Install(path); err != nil {
			t.Fatal(err)
		}
		var got struct {
			Env map[string]string `json:"env"`
		}
		data, _ := os.ReadFile(path)
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		if w := Watching(path); w != c.want || got.Env["FOO"] != c.foo {
			t.Errorf("%s: %s = %q, FOO = %q; want %q, %q", c.name, WatchEnv, w, got.Env["FOO"], c.want, c.foo)
		}
	}
}

func TestInstallReplacesOnlyOxsEntry(t *testing.T) {
	home := t.TempDir()
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(theirs), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(home)
	if err := m.Install(settings); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(settings)
	if err := m.Install(settings); err != nil {
		t.Fatal(err)
	}
	if second, _ := os.ReadFile(settings); string(second) != string(first) {
		t.Fatalf("a second install changed settings:\n%s\nthen\n%s", first, second)
	}

	var got struct {
		Model string `json:"model"`
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(first, &got); err != nil {
		t.Fatal(err)
	}
	pre := got.Hooks["PreToolUse"]
	if got.Model != "opus" || len(pre) != 2 || pre[0].Hooks[0].Command != "guard.sh" {
		t.Errorf("other settings lost: model=%q PreToolUse=%v", got.Model, pre)
	}
	if len(pre) == 2 && (pre[1].Matcher != AskMatcher || len(pre[1].Hooks) != 1 || pre[1].Hooks[0].Command != AskCommand) {
		t.Errorf("ox's ask hook = %+v, want %s on %s", pre[1], AskCommand, AskMatcher)
	}
	start := got.Hooks["SessionStart"]
	if len(start) != 2 {
		t.Fatalf("SessionStart = %+v, want their group and ox's", start)
	}
	if len(start[0].Hooks) != 1 || start[0].Hooks[0].Command != "/Users/me/bin/their-own.sh" {
		t.Errorf("their group = %+v, want only their-own.sh", start[0])
	}
	if len(start[1].Hooks) != 1 || start[1].Hooks[0].Command != Command || start[1].Hooks[0].Timeout != Timeout {
		t.Errorf("ox's group = %+v, want the built-in with a %ds timeout", start[1], Timeout)
	}
}

func TestSetupPutsSettingsBackWhenTheCheckFails(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldScript := filepath.Join(home, "hooks", "ox-brief.sh")
	if err := os.WriteFile(oldScript, []byte("#!/bin/bash\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(theirs), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewManager(home)
	m.check = func() error { return errors.New("exit status 1: unknown command \"hook\"") }
	err := m.Setup(settings)
	if err == nil || !strings.Contains(err.Error(), `unknown command "hook"`) {
		t.Fatalf("Setup = %v, want the check's failure", err)
	}
	if got, _ := os.ReadFile(settings); string(got) != theirs {
		t.Errorf("settings after a failed check:\n%s\nwant the original bytes", got)
	}
	if _, err := os.Stat(oldScript); err != nil {
		t.Errorf("the old script went with a failed check, so the restored settings can't run it: %v", err)
	}

	missing := filepath.Join(t.TempDir(), "settings.json")
	if err := m.Setup(missing); err == nil {
		t.Fatal("Setup with a failing check returned nil")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("a settings file ox created stayed after a failed check: %v", err)
	}

	m.check = func() error { return nil }
	if err := m.Setup(settings); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(settings); !strings.Contains(string(got), "hook session-start") {
		t.Errorf("settings after a passing check don't run ox hook session-start:\n%s", got)
	}
	if _, err := os.Stat(oldScript); !os.IsNotExist(err) {
		t.Errorf("ox-brief.sh kept after a passing check: %v", err)
	}
}

func TestInstallLeavesBrokenSettingsAlone(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "settings.json")
	broken := `{"model": "opus",`
	if err := os.WriteFile(settings, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewManager(t.TempDir()).Install(settings); err == nil {
		t.Fatal("Install over invalid JSON returned nil")
	}
	if got, _ := os.ReadFile(settings); string(got) != broken {
		t.Errorf("invalid settings rewritten to %s", got)
	}
}

func hookNames(m *Manager) []string {
	var names []string
	for _, h := range m.List() {
		names = append(names, h.Name)
	}
	return names
}
