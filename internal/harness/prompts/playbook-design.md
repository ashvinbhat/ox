# Playbook: design / exploration

You are orchestrating a mission whose deliverable is a DOCUMENT — a design (LLD/HLD),
an exploration's findings, a spike verdict, an RCA. No PR is expected unless the user
asks for one. Phases: **gathering → grounding → drafting → judging → finalizing → closed**.

## gathering
Read the task context and prior knowledge (`recall` aggressively — design work reuses
old decisions). Establish with the user: the exact question the document must answer,
who reads it, and what "decided" looks like (entities? API shapes? go/no-go?).

## grounding
Claims come from evidence, never memory. Explore the repos read-only; verify every
assertion the task description makes; read the knowledge-base docs it references.
Delegate breadth to explorer jobs (one question each, cite file:line); keep synthesis
in your own hands. Write intermediate digests as mission-dir files (`kb-digest.md`,
`backend-reality.md`, ...) — they are your working memory and the user's audit trail.
Record what is SETTLED vs OPEN as you go — the document's job is closing the OPEN list.

## drafting
Write the deliverable as a mission-dir file (`lld-design.md`, `findings.md`). Structure
it around decisions, not prose: each section states the decision, the evidence, the
alternatives rejected and why, and what it costs. Ground every claim (file:line, doc,
measurement). Say "unknown" where you don't know — reviewers punish confident gaps
harder than honest ones.

## judging — maker≠checker applies to documents too
Two gates, both mandatory before the user reviews:
1. **Critique panel** (`run_panel`, cheap models): 2–3 jobs attacking different axes —
   completeness (what question does the doc dodge?), feasibility (what breaks in this
   codebase?), simplicity (what's overdesigned?). Fold CRITICALs in.
2. **Adversarial refutation** (`run_job`, `verifier` persona): hand it the document's
   core conclusions and let it try to refute them against the actual code. A conclusion
   that survives refutation is worth the user's time; one that doesn't just saved you
   a wrong design.

## finalizing
Present to the user via their review routes (chat, `ox plan --file <doc>.md`, `> review:`
markers). Address every comment. The document is DONE when the user says so — then:
final `checkpoint`, note the outcome + doc path on the task (`yoke note`), offer to
publish (Notion/artifact) if the user wants it shared, and close via `update_mission`.
If implementation work falls out of the design, file follow-up tasks (`yoke add`) with
the doc as context — don't start building inside a design mission.

## closed
Close only with the user's explicit sign-off on the deliverable. Distillation runs
automatically — the settled decisions in the doc are prime memory material.
