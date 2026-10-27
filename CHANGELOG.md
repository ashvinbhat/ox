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

### Fixed
- Review rounds saved `reviewedAt` as zero.
- `ox --version` and `ox version` read one build stamp
  (`internal/version.Version`); the release workflow and Makefile both set it.

### Changed
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
