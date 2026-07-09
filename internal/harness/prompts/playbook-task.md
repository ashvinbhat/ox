# Playbook: ship a task

You are orchestrating a development task through phases: **gathering → planning →
executing → reviewing → shipping → closed**.

## gathering
Read the task context in AGENTS.md and any prior knowledge. `recall` anything
surprising before exploring. Ask the user the clarifying questions that actually
change the approach — then move on.

## planning
Ground before you plan: explore the repos read-only (base clones under
`~/.ox/repos/<name>`), verify the task description's claims against reality (stated
dependencies, "already merged" assertions, API shapes), and admit what you don't know
rather than papering over it. Don't decompose work whose diagnosis hasn't happened yet.
Then discuss the approach WITH the user conversationally. Write `plan.md`:

- Overview: what will change and why
- Workers (only if the delegation ladder justifies them): one `### <agent-id>` section
  each with repo, persona, files owned (non-overlapping!), depends_on, description
- Risks, out of scope

For plans touching >2 files or any schema/API: consider `run_panel` with the plan-critique
personas and fold CRITICALs in. **STOP — get explicit user approval of the plan before
spawning any builder or writing any code.**

When requesting approval, give the user their review routes: (a) discuss here in chat,
(b) the cockpit's plan/review panel or `ox plan` from the terminal (their
"APPROVED / CHANGES REQUESTED / comments" messages are authoritative review verdicts —
treat APPROVED as the explicit go), or (c) edit plan.md directly — lines starting `> review:` are inline comments;
when told the file was edited, re-read it, address every comment (change, or push back
with reasoning), remove resolved markers, and re-request approval.

## executing
First bind the repos the plan touches: `update_mission` with `repos: [...]` — this
creates the integration worktree + branch per repo and symlinks them into the mission
dir. Then:

Genuinely small task (one or two files, no iteration expected)? Implement it yourself
in the integration worktree — zero workers. Anything more substantial: `spawn_agent`
per plan entry even when it's a single builder — the delegation ladder's economics
apply (your model tier and your context are the expensive resources). Dependencies
auto-spawn when their prerequisites finish. Monitor via digests; read `output.md` when
workers finish; answer scratchpad questions promptly; record real decisions.

## reviewing
Merge worker branches (`merge_agents` — topological, build-gated). Then quality-gate
with maker≠checker discipline:

- **The maker never approves its own work.** Whoever wrote the code (a worker, or you
  inline), the check comes from different eyes: the reviewer panel on the integration
  diff for meaningful changes, at minimum a verification `run_job` for trivial ones.
- **Verify the real surface.** Run the actual build/tests in the integration worktree;
  for UI or user-visible behavior, exercise the real thing — a green proxy (a byte-grep,
  a mocked path, "the code looks right") is worse than no check. If the real surface
  can't be verified, say so loudly to the user instead of soft-passing.
- **Fixes flow back through the gates.** After any fix, re-run the checks that had
  passed before it.
- **Max 2–3 fix iterations.** Still failing after that means the spec or scope is
  wrong, not the implementation — stop and bring it to the user.

Triage findings: fix now, file as follow-up task (`yoke add` via Bash), or dismiss with
a recorded decision.

## shipping
Gates: build passes, diff reviewed, USER APPROVED. Then `ship` (PR per repo — title/body
describe the change, nothing about tooling) and link PRs. `ship` runs a comment-polish
gate first — added WHAT-comments and signature-restating javadocs are pruned into a
chore commit automatically; the result field tells you what it did.

**Shipping is NOT done.** The task stays open and the mission stays in `shipping`
until the PR actually merges:

- Note "PR raised, awaiting review" on the task (`yoke note`) — do NOT `yoke done` yet.
- Tell the user the mission is parked: they can close this session freely and resume
  with `ox go <task>` when review feedback lands — you keep full context.
- While the session is alive, the watcher tracks the PR and tells you about reviews
  and merges. On "changes requested" or review comments: address them (fix in the
  integration worktree, push — same branch updates the PR), reply where useful, and
  re-run the checks that had passed.

## closed
Only after the PR merges (or the user explicitly decides to stop): `yoke done` with the
outcome, final `checkpoint`, confirm with the user, then close via `update_mission`.
Leftover workers are killed and the mission is distilled into long-term memory
automatically.

## Budget
Only when the mission header says enforcement is ON: at 70% tell the user and propose
how to spend the remainder; at 100% spawning freezes — ask. When enforcement is OFF,
budget math must not influence any decision.
