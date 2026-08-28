package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var (
	opencodeModel    string
	opencodeContinue bool
)

var opencodeCmd = &cobra.Command{
	Use:   "opencode [dir] [-- opencode-args...]",
	Short: "Open opencode with ox provider keys wired in",
	Long: `Launches an interactive opencode session with ox's provider keys
(~/.ox/secrets.env) injected into the environment, so OpenRouter / Google /
etc. models work without a separately-configured shell.

Run it inside a mission worktree and opencode picks up that worktree's
opencode.json (model + ox MCP) automatically. Run it anywhere else and it is
plain opencode with your keys available.

Examples:
  ox opencode                                  # here, default model
  ox opencode -m openrouter/z-ai/glm-4.7-flash
  ox opencode -c                               # continue the last session here
  ox opencode ~/code/foo -- --agent build      # a dir, plus raw opencode flags`,
	Args: cobra.ArbitraryArgs,
	RunE: runOpencode,
}

func runOpencode(cmd *cobra.Command, args []string) error {
	if _, err := exec.LookPath("opencode"); err != nil {
		return fmt.Errorf("opencode not found on PATH — install it first")
	}

	// A leading positional that names an existing directory is the working
	// dir; everything else (typically after `--`) forwards verbatim to
	// opencode.
	dir := ""
	var passthrough []string
	for _, a := range args {
		if dir == "" && !strings.HasPrefix(a, "-") {
			if info, err := os.Stat(a); err == nil && info.IsDir() {
				dir = a
				continue
			}
		}
		passthrough = append(passthrough, a)
	}
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		dir = wd
	}

	var ocArgs []string
	if opencodeModel != "" {
		ocArgs = append(ocArgs, "-m", opencodeModel)
	}
	if opencodeContinue {
		ocArgs = append(ocArgs, "--continue")
	}
	ocArgs = append(ocArgs, passthrough...)

	oc := exec.Command("opencode", ocArgs...)
	oc.Dir = dir
	oc.Stdin = os.Stdin
	oc.Stdout = os.Stdout
	oc.Stderr = os.Stderr
	oc.Env = append(os.Environ(), loadSecretsEnv()...)

	fmt.Printf("🐂 opencode in %s\n", dir)
	return oc.Run()
}

// loadSecretsEnv reads ~/.ox/secrets.env into KEY=VALUE entries so opencode's
// {env:...} provider-key references resolve in the launched process.
func loadSecretsEnv() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(home, ".ox", "secrets.env"))
	if err != nil {
		return nil
	}
	var env []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimPrefix(strings.TrimSpace(line), "export ")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if key, val, ok := strings.Cut(line, "="); ok {
			env = append(env, key+"="+strings.Trim(val, `"'`))
		}
	}
	return env
}

func init() {
	opencodeCmd.Flags().StringVarP(&opencodeModel, "model", "m", "", "opencode model (provider/model)")
	opencodeCmd.Flags().BoolVarP(&opencodeContinue, "continue", "c", false, "continue the last session in this directory")
	rootCmd.AddCommand(opencodeCmd)
}
