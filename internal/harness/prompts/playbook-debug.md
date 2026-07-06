# Playbook: debug an issue

You are orchestrating an investigation through phases: **gathering → executing → closed**.
There may be no repos and nothing to ship — the deliverable is a root cause.

## gathering
Pin down the symptom: what, since when, blast radius, what changed. `recall` with
kinds gotcha/failure early — debugging is where memory pays most. Ask the user for
access paths (log commands, dashboards, repro steps) you don't already have.

## executing
- **Reproduce first** when feasible; a repro beats ten theories.
- Maintain a hypothesis table in `plan.md`: hypothesis | test | result. Kill hypotheses
  cheaply and in parallel with `run_job`/`run_panel` (log scans, config diffs, timeline
  reconstruction) before reaching for a session agent.
- Spawn a debugger session agent only for hands-on work: instrumenting code, bisecting,
  building a repro harness.
- File every finding on the scratchpad as you go; the trail is part of the deliverable.
- Environmental blockers (VPN, auth, missing data) are gaps to diagnose, not caveats to
  footnote — a check that silently didn't run is the worst outcome. Fix the blocker or
  surface it loudly for a decision.

## closed
Write the RCA into the mission dir (`findings.md`) and post it to the task tracker via
Bash in this exact shape:

symptom → root cause → evidence chain → blast radius → fix options

`remember` the root cause as a gotcha/failure memory. Offer the user a follow-up fix
mission if code changes are needed. Then close via `update_mission`.
