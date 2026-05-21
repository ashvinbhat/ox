package review

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// AgentSpec describes one reviewer pass: a name, the focus prompt, and the
// findings file the agent must write to.
type AgentSpec struct {
	Name         string // "correctness", "security", "design", "test"
	Focus        string // short description, embedded in the prompt
	Instructions string // pass-specific instructions (what to look for)
}

// CorrectnessAgent reviews for logic bugs, edge cases, error handling.
var CorrectnessAgent = AgentSpec{
	Name:  "correctness",
	Focus: "logic bugs, edge cases, error handling",
	Instructions: `Focus on correctness:
- Logic bugs: incorrect branching, wrong operators, swapped arguments, off-by-one
- Edge cases: nil/empty/zero inputs, boundary values, unexpected types
- Race conditions / ordering: shared state, async coordination, sequencing assumptions
- Error handling: swallowed errors, missing error paths, incorrect error propagation
- Resource handling: leaked handles, missing cleanup, partial-failure rollback

Do NOT comment on style, naming, or design unless it produces a correctness bug.
Do NOT comment on test coverage gaps — a separate agent handles that.`,
}

// SecurityAgent reviews for security and privacy concerns.
var SecurityAgent = AgentSpec{
	Name:  "security",
	Focus: "auth boundaries, injection, secrets, OWASP top 10, dependency risk",
	Instructions: `Focus on security and privacy:
- Auth boundaries: missing authn/authz, IDOR, privilege escalation, tenant isolation
- Injection: SQL, NoSQL, command, template, prompt — anywhere untrusted input flows into a sink
- Secrets: hardcoded credentials, tokens in logs, secrets sent to third parties, secrets in test fixtures that look real
- Untrusted input: missing validation, unsanitized output, deserialization risk, SSRF/redirect open
- Crypto: weak/missing transit/at-rest, predictable randomness, broken key handling
- Dependency / supply chain risk: new dependencies, version bumps to known-bad versions
- PII / privacy: PII leaking into logs, telemetry, or third-party calls

Do NOT flag general code quality concerns. Do NOT flag missing tests.
If the diff is purely cosmetic (docs / formatting / renames with no semantic change), it is OK to produce zero findings.`,
}

// DesignAgent reviews for architecture, abstraction, conventions, authoring-guide compliance.
var DesignAgent = AgentSpec{
	Name:  "design",
	Focus: "architecture, abstractions, conventions, authoring-guide compliance",
	Instructions: `Focus on design and convention:
- Layering: violations (controller doing repository work, model knowing about HTTP, etc.)
- Abstractions: leaky, premature, or missing — three duplicated lines vs a one-off
- Coupling / cohesion: things that should be together but aren't (or vice versa)
- Naming: misleading, ambiguous, or inconsistent with repo conventions
- API shape: surface ergonomics, parameter order, optionality, error contracts
- Convention adherence: deviations from patterns in nearby files / CLAUDE.md / repo style
- Agent-authoring-guide (when the diff touches agentic surfaces in frontend/agentic/ or frontend/.agents/):
  - Narrowest lever first (system prompt vs skill vs tool vs code/schema)
  - No duplication across levers
  - Enforce invariants in code, not prompts
  - Examples are behavior — audit example dialogue for unintended patterns
  - "When NOT to use" sections present and accurate on new/changed tools

Do NOT flag straight bugs (correctness handles those). Do NOT flag missing tests.`,
}

// TestAgent reviews for test coverage, test quality, and regression coverage.
var TestAgent = AgentSpec{
	Name:  "test",
	Focus: "test coverage, weak assertions, missing regression tests",
	Instructions: `Focus on test coverage and quality:
- New code without any test exercising it
- New error / edge / empty paths lacking explicit coverage
- Tautological or vacuous assertions (asserting on something the test itself constructed, asserting on truthiness only)
- Mocked-too-much tests that wouldn't catch the real regression
- Bug fixes shipping without a regression test that fails on the old code
- Tests that change behavior assertions to match new code without justification (test was the spec)
- Missing integration coverage when the PR adds a new endpoint / workflow seam

Flag the missing test by anchoring to the line in the changed source file that lacks coverage (not in the test file, since the test doesn't exist yet). For weak existing tests, anchor to the assertion line in the test file.

Do NOT flag correctness bugs, security issues, or design concerns — those are handled by other agents.`,
}

// AllAgents is the v1 default fan-out: four parallel reviewers.
var AllAgents = []AgentSpec{
	CorrectnessAgent,
	SecurityAgent,
	DesignAgent,
	TestAgent,
}

// RunAgent invokes one reviewer pass against the prepared review workspace
// and returns its output (findings and, on follow-up runs, addressing
// verdicts on prior findings). Output (stdout/stderr) is captured to
// logPath if non-empty, otherwise streamed to the calling terminal.
//
// The agent runs with the worktree as cwd, the REVIEW.md as primary context,
// and a strict instruction to write its output to
// .ox/review/findings/<agent>.json before exiting. priorRefs lists the
// finding refs assigned to this agent's prior findings; when non-empty the
// agent is told to also produce addressing entries for each.
func RunAgent(worktree *ReviewWorktree, spec AgentSpec, logPath string, priorRefs []string) (AgentOutput, error) {
	reviewDir := filepath.Join(worktree.Path, ".ox", "review")
	findingsPath := filepath.Join(reviewDir, "findings", spec.Name+".json")

	// Pre-create an empty output file so we can detect failure-to-write vs.
	// agent-decided-no-findings (the agent overwrites this).
	if err := os.WriteFile(findingsPath, []byte(`{"findings":[],"addressing":[]}`), 0o644); err != nil {
		return AgentOutput{}, fmt.Errorf("init findings file: %w", err)
	}

	prompt := buildAgentPrompt(spec, findingsPath, reviewDir, priorRefs)

	cmd := exec.Command(
		"claude",
		"--dangerously-skip-permissions",
		"--append-system-prompt", prompt,
		"-p", "Read .ox/review/REVIEW.md and produce your findings now. Write the JSON array to "+findingsPath+" and then exit.",
	)
	cmd.Dir = worktree.Path

	if logPath != "" {
		// Parallel run: capture output to log file so we don't interleave terminals.
		logFile, err := os.Create(logPath)
		if err != nil {
			return AgentOutput{}, fmt.Errorf("create %s log: %w", spec.Name, err)
		}
		defer logFile.Close()
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		// No stdin for headless runs.
	} else {
		// Single-agent / interactive: stream to terminal.
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Run(); err != nil {
		return AgentOutput{}, fmt.Errorf("run %s agent: %w", spec.Name, err)
	}

	return readAgentOutput(findingsPath, spec.Name, priorRefs)
}

func buildAgentPrompt(spec AgentSpec, findingsPath, reviewDir string, priorRefs []string) string {
	followUpSection := ""
	if len(priorRefs) > 0 {
		followUpSection = fmt.Sprintf(`

# Follow-up review mode
This is a FOLLOW-UP review. Your prior review of this PR produced findings
labeled %s. They are listed in REVIEW.md under "Prior review findings (this agent)".
The "Diff since last review" section in REVIEW.md shows what the author has changed
since you last reviewed.

For each prior finding, output an entry in the addressing[] array with:
- ref:    the prior finding label (e.g. "F1")
- status: one of "addressed", "partial", "ignored"
- note:   1-2 sentences explaining your verdict, referencing the addressing diff
- agent:  must be exactly %q

Status guide:
- addressed: the change since last review fully resolves the concern
- partial:   the area was touched but the concern is not fully fixed
- ignored:   no relevant change to the file/line the prior finding pointed at

When producing new findings[], DO NOT re-raise findings that are still
present from your prior review unless their status was "addressed" (the author
fixed the original spot and re-introduced the issue elsewhere). Focus new
findings on the diff since last review.`, strings.Join(priorRefs, ", "), spec.Name)
	}

	return fmt.Sprintf(`You are a code reviewer running as the %q pass.

# Your focus
%s

# Anchoring rule (HARD)
**Every finding MUST be anchored to a specific file and line in the diff.**
There are NO PR-level or file-less comments. If a concern spans multiple files,
pick the single most representative file:line to anchor it to and list the
others in the body. If a concern is upstream of the diff (outside the touched
lines), anchor it to the nearest in-diff line and explain the real location in
the body. If you cannot anchor a finding to a line in the diff, do not raise
it.

# Output format (HARD)
Write a JSON OBJECT to: %s
The object MUST have these top-level keys:
- findings: array of finding objects (see schema below). May be empty.
- addressing: array of addressing objects (see schema below). Empty unless follow-up mode.

Each finding object MUST have these keys:
- file: string (path from repo root, must match a file in the diff)
- line: integer (line number in the new version of the file, must fall within a diff hunk)
- endLine: integer (optional, for multi-line findings)
- severity: one of "blocker", "issue", "suggest", "nit"
- category: one of "security", "correctness", "design", "test", "naming", "perf", "docs"
- agent: must be exactly %q
- title: short one-line summary
- body: longer explanation. May reference other files/lines in prose.

Severity guide:
- blocker: ship-stopper — wrong behavior, data loss, security hole
- issue: real bug or significant concern, must address before merge
- suggest: meaningful improvement, author's call
- nit: minor polish (style, naming) — only include if genuinely useful

# Process
1. Read %s/REVIEW.md fully. It contains PR title/body, files changed, repo conventions, and the unified diff.
2. Open any file from the diff in this worktree if you need fuller context — the working directory is the PR head checkout.
3. Think carefully. Prioritize quality over quantity. Five high-signal findings beat fifteen noisy ones.
4. Write the JSON array to %s. Overwrite, do not append. Do not write anything else to that file (no markdown fences, no commentary).
5. Exit when done.

If you have nothing to flag, write %q to the file and exit. That is a valid and acceptable outcome.

DO NOT:
- Post comments via gh or any tool — your only output channel is the JSON file.
- Modify any source files in the worktree.
- Run tests, builds, or other long-running commands unless absolutely necessary for context.%s`,
		spec.Name,
		strings.TrimSpace(spec.Instructions),
		findingsPath,
		spec.Name,
		reviewDir,
		findingsPath,
		`{"findings":[],"addressing":[]}`,
		followUpSection,
	)
}

func readAgentOutput(path, agentName string, priorRefs []string) (AgentOutput, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return AgentOutput{}, fmt.Errorf("read findings: %w", err)
	}
	cleaned := stripCodeFence(strings.TrimSpace(string(b)))

	// Accept both the new object shape and the legacy bare-array shape (which
	// older mid-run files may still have after the schema migration).
	var output AgentOutput
	if err := json.Unmarshal([]byte(cleaned), &output); err != nil {
		// Try the legacy array shape.
		var arr []Finding
		if jerr := json.Unmarshal([]byte(cleaned), &arr); jerr == nil {
			output.Findings = arr
		} else {
			return AgentOutput{}, fmt.Errorf("parse agent output from %s: %w", path, err)
		}
	}

	// Enforce the anchoring rule on findings.
	validFindings := output.Findings[:0]
	for _, f := range output.Findings {
		if f.File == "" || f.Line <= 0 {
			fmt.Fprintf(os.Stderr, "warning: dropping unanchored finding from %s agent: %q\n", agentName, f.Title)
			continue
		}
		if f.Agent == "" {
			f.Agent = agentName
		}
		validFindings = append(validFindings, f)
	}
	output.Findings = validFindings

	// Enforce that addressing entries only reference known prior refs and have
	// valid statuses. Drop anything malformed with a warning.
	validRefs := map[string]bool{}
	for _, r := range priorRefs {
		validRefs[r] = true
	}
	validAddressing := output.Addressing[:0]
	for _, a := range output.Addressing {
		if !validRefs[a.Ref] {
			fmt.Fprintf(os.Stderr, "warning: dropping addressing with unknown ref %q from %s agent\n", a.Ref, agentName)
			continue
		}
		switch a.Status {
		case AddressingAddressed, AddressingPartial, AddressingIgnored:
		default:
			fmt.Fprintf(os.Stderr, "warning: dropping addressing %s with invalid status %q from %s agent\n", a.Ref, a.Status, agentName)
			continue
		}
		if a.Agent == "" {
			a.Agent = agentName
		}
		validAddressing = append(validAddressing, a)
	}
	output.Addressing = validAddressing

	return output, nil
}

func stripCodeFence(s string) string {
	// Some models wrap JSON in ```json … ``` despite being told not to.
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Drop first line (the opening fence).
	if nl := strings.Index(s, "\n"); nl >= 0 {
		s = s[nl+1:]
	}
	// Drop trailing fence.
	s = strings.TrimSuffix(strings.TrimRight(s, "\n"), "```")
	return strings.TrimSpace(s)
}
