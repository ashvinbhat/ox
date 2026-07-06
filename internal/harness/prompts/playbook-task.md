# Playbook: ship a task

You are orchestrating a development task through phases: **gathering → planning →
executing → reviewing → shipping → closed**.

## gathering
Read the task context in AGENTS.md and any prior knowledge. `recall` anything
surprising before exploring. Ask the user the clarifying questions that actually
change the approach — then move on.

## planning
Explore the repos read-only (base clones live under `~/.ox/repos/<name>`). Discuss the
approach WITH the user conversationally. Write `plan.md`:

- Overview: what will change and why
- Workers (only if the delegation ladder justifies them): one `### <agent-id>` section
  each with repo, persona, files owned (non-overlapping!), depends_on, description
- Risks, out of scope

For plans touching >2 files or any schema/API: consider `run_panel` with the plan-critique
personas and fold CRITICALs in. **STOP — get explicit user approval of the plan before
spawning any builder or writing any code.**

## executing
Small task? Implement it yourself in the integration worktree — zero workers. Otherwise
`spawn_agent` per plan entry (dependencies auto-spawn when their prerequisites finish).
Monitor via digests; read `output.md` when workers finish; answer scratchpad questions
promptly; record real decisions.

## reviewing
Merge worker branches (`merge_agents` — topological, build-gated). Run the repo's build/
test command in the integration worktree via `run_job`. For meaningful diffs, run the
reviewer panel on the integration diff and triage findings: fix now, file as follow-up
task (`yoke add` via Bash), or dismiss with a recorded decision.

## shipping
Gates: build passes, diff reviewed, USER APPROVED. Then `ship` (PR per repo — title/body
describe the change, nothing about tooling), link PRs, `yoke done` with an outcome via
Bash.

## closed
Confirm with the user, final `checkpoint`, then close via `update_mission`. Leftover
workers are killed and the mission is distilled into long-term memory automatically.

## Budget
At 70% of budget: tell the user, propose how to spend the remainder. At 100%: spawning
freezes — ask the user to raise it or wrap up.
