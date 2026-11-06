# Changelog

All notable changes to this project are documented here.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Toward 1.0.0. ox becomes what Claude Code desktop threads draw on; the tmux
mission harness is frozen.

### Added
- `ox now`: every desktop thread by state (needs you, in review, working,
  stuck, done) and track, plus yoke tasks and open PRs that no thread owns.
  `--brief` for thread start, `--json` for agents, `--track` to filter,
  `--all` to include archived threads.
- Tracks: `~/.ox/tracks.yaml` names your workstreams; terms match at a word start.
- `ox-brief` SessionStart hook: every new thread starts with `ox now --brief`.
- `ox worktrees prune`: removes worktrees (and local branches) whose PR merged,
  keeping any with uncommitted files, local-only commits, or an open thread in
  them. The `ox-brief` hook runs it in the background at most hourly; removals
  go to `~/.ox/logs/worktrees.log`.

- Review desk. Reviews start only from an explicit ask; team review requests
  are never pulled in.
  - `/review-asks` skill: finds review asks in Slack DMs, group DMs and
    @-mentions (Slack connector) and records them with `ox review ask`.
  - `/review-pr` skill: reviews against the whole codebase at the PR head
    (callers, siblings, consumers, duplication, tests, conventions), grades
    open findings on a re-review, and posts only the text the user approved.
  - `ox reviews`: the queue (to review, re-review, waiting on author),
    refreshed from GitHub; merged and closed PRs are never re-checked.
  - `ox review start`: full checkout at the PR head, the diff, and on a
    re-review the interdiff since the last reviewed head plus open findings.
  - `ox review post --confirm` / `ox review record`: save a round, posted or not.
  - Ledger (`~/.ox/reviews/`): asks per PR, base SHA per round, verdicts per
    round; finding refs continue across rounds (F4 after F1–F3), and "open"
    means every finding not yet marked addressed.
  - `ox now` shows reviews to do; the brief reads the ledger without network.
- `ox setup`: installs the `ox-brief` hook and ox's skills in one step;
  re-run after every upgrade. A skill folder you wrote yourself is left alone.
- `ox worktrees prune` also removes `review/pr-<n>` checkouts once that PR
  merges or closes.

- Memory check. Claude Code's auto-memory is the one store.
  - `ox memory audit`: flags `~/` paths, code names, source files and
    `ox` commands a memory mentions that no longer exist; MEMORY.md links
    to missing files, files it misses, and length past 200 lines; project
    memories untouched for 30 days; memories stranded in other projects'
    stores. Code is searched in every checkout, then at the default branch
    tip, so a stale checkout doesn't make live code look gone.
  - Citations: `metadata.cites` (`repo:path`, `repo:Symbol`, `~/path`) are
    checked exactly; `metadata.verified` resets the stale clock.
  - `--keep <id>` marks a suggestion fine as is, for good.
  - The `ox-brief` hook runs the audit weekly in the background; the brief
    shows "Memory: N suggestions" and `ox now` a MEMORY section.
  - `/memory-check` skill: checks each suggestion, proposes edit, delete,
    merge, move, verify or keep in batches of five, and applies only what
    you approve.

- Learning loop.
  - Decision journal: `ox journal add --decided … --over … --why …` appends
    to `~/.ox/journal/YYYY-MM-DD.md`, with the time, thread title and track
    taken from the desktop thread it runs in. `ox journal [--days N]
    [--track x]` reads it; CLAUDE.md asks every thread to log real choices.
  - `/own-it` skill: Ashvin predicts the approach (where, shape, risk), Claude
    builds, then compares and explains each gap; graded match, partial or
    miss and logged with `ox journal own-it`. Tracks with `own: true` in
    `tracks.yaml` get offered a round, at most 2 a day.
  - `/recap` skill: three concepts from the week, one explain-it-back prompt
    each, gaps filled, logged with `ox journal recap`. The brief shows
    "Recap due" Friday through Monday until it's done; `ox now` has a JOURNAL
    section.
  - `ox journal status`: today's decisions and own-it rounds, the week's
    own-it score, whether a recap is due, and this thread's track.

- Asks queue.
  - `ox asks`: one ranked queue of what only you can do: thread questions,
    review asks, your PRs ready to merge (approved or in your own repo, CI
    green), memory suggestions, a due recap. Oldest first within each; a
    thread question older than 3 days is marked "still needed?". Rough
    minutes per ask.
  - PR conflicts, failing CI and requested changes are listed apart as
    "Threads can fix", with the thread that owns the PR.
  - `ox asks drop <id>` hides an ask until it changes.
  - PR state is fetched with gh, cached in `~/.ox/asks/prs.json`, and
    refreshed in the background by the `ox-brief` hook and the board once
    it's 10 minutes old. A claim file lets one refresh run at a time.
  - The brief's "Needs you", reviews, memory and recap lines become one
    "Waiting on you" list (top three) plus a "Threads can fix" line; `ox now`
    opens with the full queue.
  - Plain chat: CLAUDE.md maps what Ashvin says ("what's waiting on me?",
    "recap", "clean up my threads") to the command or skill a thread runs;
    threads never tell him to run commands. `ox now --brief --mention` (the
    hook) adds a "Mention once" line to the first brief of the day for a due
    recap or memory suggestions, which the thread offers in words.
  - `/tidy` skill: proposes archiving finished threads, renaming threads that
    share a title, a sidebar group per track, and what to do with stale
    questions; applies what you approve with the app's session tools.

- Board panel. `ox setup` installs `ox-board`, a Claude Code plugin, into
  `~/.claude/skills/ox-board`. Beside every desktop thread it lists only the
  threads that need you, each with one line on what it's waiting on: a
  question you haven't dropped, a PR ready to merge, or a turn that went
  quiet. No buttons; it reads like a chat reply (Markdown). Every open panel
  rereads `ox now --brief --json` every 15 seconds, so a change in one
  thread shows in the others; terminal and headless sessions never poll.
- Board: a small × beside each item drops that one, and Clear all under the
  list drops every item shown, for every panel. Each comes back when its thread asks something new or, for a stuck
  thread, does anything again. `ox asks drop stuck:<thread id>` hides a stuck
  thread; `ox now --json` marks it `dropped`.
- `ox now --json` lists the tracks by name, and each ask carries its track
  and the id of the thread it belongs to.

- Parking. `/park` skill: when you leave a thread on purpose, it writes a
  resumption note as the thread's last message (goal, done, next, open,
  state) and runs `ox park --next "…"`, which records the next step in
  `~/.ox/parked/`. `ox now` shows the thread as "parked" with that step until
  it is active again; a parked thread is never an ask, never archivable, and
  `/tidy` leaves it alone. The brief counts parked threads.
- Memory check, three more checks.
  - `changed`: a cited file (or, for a cited symbol, the file named after it)
    has commits on the default branch since the memory was last confirmed or
    edited; a cited `~/` file was edited since.
  - `duplicate`: two memories whose words overlap enough (TF-IDF cosine
    0.4+) that one may restate the other.
  - `frontmatter`: YAML that doesn't parse, which made the audit read the
    memory as having no type, verified date or cites.
- `/open-pr` skill: formats, starts the targeted tests in the background,
  opens the PR as an unlabelled draft while they run, and once they pass
  locally marks it ready for review and adds `ready-for-review` on CBX1
  repos. A red run keeps it a draft. It's ox's own, so repos' shared PR
  skills stay as they are.
- Board cleanup. "Clean up the board" runs `/tidy` on the board's items only.
  For each thread question it offers drop, archive, or passing on the answer
  you give in chat, word for word. It recommends dropping a question that
  only repeats another thread's. A thread marked stuck whose last turn
  finished gets archived; one that stopped mid-work can be told to carry on.
- Background jobs (worktree prune, memory audit, PR refresh) keep a status in
  `~/.ox/jobs/`. Each runs at most once per interval, retries a failure after
  5 minutes, and is killed past its time limit. Its lock names the holder's
  pid, so a crashed run never blocks the next. Two failures in a row put a
  line in the brief and on the board, such as "PR state is 2h old: gh auth
  login", until the job succeeds again.
- `ox setup` runs the start hook the way a thread would, with a bare PATH,
  and puts `settings.json` back as it was if the hook fails.
- Word budget: `ox memory audit` adds up what every thread loads at start
  (the user and project CLAUDE.md files, and MEMORY.md's first 200 lines).
  Past 3,000 words it flags a `budget` suggestion naming the largest
  sections, and `/memory-check` proposes cuts that move rules rather than
  drop them.
- Tests can't reach live paths. Every lookup of the home folder, `OX_HOME`
  and `CLAUDE_CONFIG_DIR` goes through `internal/livepath`, which panics
  under `go test` for a path outside the temp dir. Three tests that read the
  real home now use a temp one.
- `make mutate` (and CI) runs `tools/mutate/mutations.yaml`: each entry
  turns one rule off in a copy of the repo, and its package's tests must
  fail. A survivor, a mutation that doesn't compile, or an entry whose code
  is gone fails the run. It starts with 14 rules from the last five PRs.
- A thread's question asked before its PR merged is marked "ox#35 merged
  since; still needed?" in `ox asks`, the brief and the board. The PR
  refresh also reads your PRs merged in the last two weeks. `/tidy`
  recommends archive when the merge answered the question, else drop.
- Fix ladder. A feedback memory is written when Ashvin states a rule or
  corrects the same thing twice; a one-off goes in the journal. Each time a
  covered rule is broken again, its day goes in `metadata.misses`, and
  `ox memory audit` flags it `missed` until the rule moves up a rung: a
  line in the skill whose step it governs, then a check in code
  (`metadata.rung`, `metadata.promoted`). `/memory-check` proposes the move.
- Work list (`docs/work-list.md`). `ox work refresh` runs `yoke sync`
  hourly, which brings in your current-sprint Notion tickets. Every 10
  minutes it also keeps a yoke task for each thread with a PR. The task is
  keyed by the thread and its PRs, filed under the ticket a PR names,
  started while a PR is open and done once its PRs merged, with their titles
  as the outcome. A PR belongs to the thread that opened it. The start hook
  runs it, and the brief shows "Tickets: N open · Findings: N waiting for
  your go" plus the thread's own key and work. CLAUDE.md gains two rules:
  lasting work gets a task, and anything noticed and not done, or promised
  for later, is a finding in the same reply.

### Fixed
- `ox setup` replaced every SessionStart hook in `~/.claude/settings.json`
  with ox's. It now replaces only ox's entry and keeps the rest.
- `ox now` called a thread stuck when it was opened and never used, such as
  a side chat forked from another thread and left. Nothing ran in it, so it
  is done now, and the board no longer lists it.
- Review rounds saved `reviewedAt` as zero.
- `ox --version` and `ox version` read one build stamp
  (`internal/version.Version`); the release workflow and Makefile both set it.

### Changed
- CLAUDE.md's commands, code map, `~/.ox` layout and common issues moved to
  `internal/CLAUDE.md`, which Claude Code loads only when a thread opens a
  file under `internal/`. Every thread loads about 750 fewer words: 2,614,
  under the 3,000 budget.
- The start hook is `ox hook session-start`, ox itself, not a bash script. It
  needs no `jq`, has a 15s timeout, and prints nothing on a panic, a missing
  config or a brief slower than 10s. It adds brew's and Go's bin folders to
  PATH, so the jobs find `gh`. `ox setup` deletes `~/.ox/hooks/ox-brief.sh`
  once the new hook passes its check.
- A failed `gh` call in the PR refresh or the worktree prune carries gh's own
  message instead of an exit status.
- `/review-pr` checks that a change can do what it is for, reads red CI to its
  cause, applies the repo's own skills, looks for the test that turns guarded
  code on through real wiring, checks claims in the PR body, draws a
  walkthrough of changes with a flow, and ends each round by proposing edits
  to itself.
- `/review-pr` recommends approve when no finding blocks the merge, looks for
  every other way traffic leaves when a change hooks into one choke point, and
  checks every flag the new code sits behind.
- A thread's track comes from its title first, then its PRs' repos and
  branches, so an ox thread that opened a review-desk PR stays on ox.
- **Breaking:** bare `ox` runs `ox now` instead of resuming a mission. Use
  `ox go` to resume one.
- CLAUDE.md and README lead with working in desktop threads; missions are
  documented as frozen.

### Removed
- The `ox-instructions` and `yoke-ready-tasks` hooks, which pointed threads at
  missions. `ox hooks init` deletes their scripts.
- **Breaking:** `ox learn`. `~/.ox/memory.db` is an archive;
  `ox learnings` and `ox memory recall` still search it.

## [0.1.0] - 2026-08-19

Initial public release.

### Added
- Mission harness: `ox go <task|mission|goal>` opens a persistent orchestrator
  session that plans, delegates to worker/job agents, integrates, and ships —
  resumable via pre-assigned Claude session ids.
- Typed MCP tool surface per session (`ox mcp`) for orchestrator and worker roles.
- Watcher: reconciles agent/job state, dependency-gated spawning, transcript cost
  tracking, PR polling, and guarded event delivery to the orchestrator.
- Layered memory (`~/.ox/memory.db`): FTS5 + embeddings with hybrid retrieval, and
  living per-repo knowledge docs maintained by a close-time distiller.
- Playbooks (task, debug, review, design) as markdown — new mission type = new file.
- Personas (builder, explorer, reviewer panel, verifier, fixer, distiller, …).
- `ox where`, `ox inbox`, `ox retro`, `ox plan`, `ox missions` for locating work,
  triage, per-mission scorecards, plan review, and lifecycle.

[Unreleased]: https://github.com/ashvinbhat/ox/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/ashvinbhat/ox/releases/tag/v0.1.0
