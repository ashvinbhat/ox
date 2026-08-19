# Changelog

All notable changes to this project are documented here.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
