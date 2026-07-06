// Package review implements the PR review workflow: resolve PR, create
// review worktree, run reviewer agents, collect findings, render and post.
package ghreview

// Severity classifies a finding's importance.
type Severity string

const (
	SeverityBlocker Severity = "blocker"
	SeverityIssue   Severity = "issue"
	SeveritySuggest Severity = "suggest"
	SeverityNit     Severity = "nit"
)

// Category groups findings by concern.
type Category string

const (
	CategorySecurity    Category = "security"
	CategoryCorrectness Category = "correctness"
	CategoryDesign      Category = "design"
	CategoryTest        Category = "test"
	CategoryNaming      Category = "naming"
	CategoryPerf        Category = "perf"
	CategoryDocs        Category = "docs"
)

// Finding is one reviewer observation, always anchored to a file:line in the PR diff.
//
// File and Line are required. If an agent has a cross-file or upstream-of-diff
// concern, it anchors to the closest in-diff line and explains the real
// location in Body. The aggregator rejects any finding missing File or Line.
type Finding struct {
	File     string   `json:"file"`              // path from repo root
	Line     int      `json:"line"`              // anchor line in the diff
	EndLine  int      `json:"endLine,omitempty"` // optional multi-line range end
	Severity Severity `json:"severity"`
	Category Category `json:"category"`
	Agent    string   `json:"agent"` // which reviewer produced this
	Title    string   `json:"title"`
	Body     string   `json:"body"`

	// Ref is a stable label assigned at save-time (F1, F2, ...) so a future
	// follow-up review can reference this finding by name in its addressing
	// output. Not present during the first render; populated by SaveState.
	Ref string `json:"ref,omitempty"`

	// CommentID is the GitHub pull_request_review_comment id assigned when
	// this finding was posted. Used by follow-up reviews to post replies to
	// the original comment thread. Zero when the finding was not posted
	// (e.g. self-PR, --no-interactive, user chose 'none').
	CommentID int64 `json:"commentID,omitempty"`
}

// Review is the full set of findings from one or more reviewer agents,
// plus an optional global comment for cross-cutting commentary that
// genuinely cannot be anchored to a single line.
type Review struct {
	PRNumber      int       `json:"prNumber"`
	PRURL         string    `json:"prUrl"`
	Findings      []Finding `json:"findings"`
	GlobalComment string    `json:"globalComment,omitempty"`
}

// AddressingStatus is one agent's verdict on whether a prior finding has
// been addressed in the diff since the last review.
type AddressingStatus string

const (
	AddressingAddressed AddressingStatus = "addressed"
	AddressingPartial   AddressingStatus = "partial"
	AddressingIgnored   AddressingStatus = "ignored"
)

// Addressing is an agent's grading of one prior finding from the last review.
type Addressing struct {
	Ref    string           `json:"ref"`    // matches prior Finding.Ref (e.g. "F1")
	Status AddressingStatus `json:"status"` // addressed | partial | ignored
	Note   string           `json:"note"`   // short, 1-2 sentence rationale
	Agent  string           `json:"agent"`  // grading agent (stamped at read-time if missing)
}

// AgentOutput is the JSON shape an agent writes to its findings file.
// In follow-up mode, both findings[] (new) and addressing[] (verdicts on
// prior findings) may be populated. In first-review mode, only findings[].
type AgentOutput struct {
	Findings   []Finding    `json:"findings"`
	Addressing []Addressing `json:"addressing,omitempty"`
}

// Event mirrors the GitHub review event enum.
type Event string

const (
	EventComment        Event = "COMMENT"
	EventApprove        Event = "APPROVE"
	EventRequestChanges Event = "REQUEST_CHANGES"
)

// Selection is what gets posted: which findings as inline comments, the
// review event, an optional global comment, and (follow-up rounds) which
// addressing verdicts go out as replies on prior comment threads.
type Selection struct {
	Findings      []Finding
	Event         Event
	GlobalComment string
	Addressing    []Addressing
	PriorByRef    map[string]Finding // ref → prior finding (incl. CommentID) for reply posting
}
