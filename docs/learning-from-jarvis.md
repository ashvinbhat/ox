# What ox can learn from Jarvis v3 (2026-11-05)

Source: Arun's `arun-main-v3` branch of CBX1/jarvis-ai-assistant (active the
same day), and CBX1/skills, where its 95 skill folders link. About 500 files
in the branch: roughly 60 hook handlers, 43 plumbing scripts, beads as the
task store. Mechanisms only, in ox's own words.

## The short version

Jarvis is much bigger than ox, and its own records count the cost. A week of
Arun's inputs to Jarvis was 28% spent running the engine. Its always-loaded
rules are about 3,900 words, over its own 3,000-word budget. Two ways of
running work exist side by side mid-migration. The parts worth taking are
its mechanical safety nets, not its scale.

## Take

| # | Mechanism | What Jarvis does | ox today |
|---|---|---|---|
| 1 | Loud background failures | A failing job raises one notice and clears it on recovery. Two clocks per job: last success sets the cadence, last attempt paces retries (5 min). Success is stamped only when the job really succeeded. | The brief and board never say how old PR state is (`asks.go:228`). If `gh` fails, merge asks go stale silently. The worktree prune stamps before it works (`worktrees.go:55`), so a failed run waits an hour unseen. |
| 2 | Start check after install | After an install, run `claude -p` with hooks and without. If only the hooked start fails, a hook broke it: roll back. | `ox setup` assumes the hook works. A broken hook would break every new thread's brief. |
| 3 | Hooks run through one program | One dispatcher runs each handler with its own time limit, kills hung ones, and treats a crash as "skip", never "block". It logs every block and skip. | `~/.ox/hooks/ox-brief` is bash and needs `jq`. Without `jq` there's no brief, and nothing says so. |
| 4 | Tests that prove each rule matters | Mutation tests turn one rule off and require a case to fail. Tests refuse to touch live paths. | Done by hand once (#32). No guard against a test reading the real `~/.ox`. |
| 5 | One task per real thing | A task needs an identity (`CB-n`, `pr:repo-n`). Creating one first looks for that identity and reopens instead of duplicating. Findings link back to the work they came from. A promised "later" step becomes a task in the same turn. | Fits the work-list design (`docs/work-list.md`); not there yet. |
| 6 | Stale-ask sweep | An ask is withdrawn when its source settles elsewhere (task closed, PR merged after the ask). Doubtful cases are flagged, not decided. | Asks lapse only when they change; a question is marked "still needed?" after 3 days. |
| 7 | A ladder for fixes | A learning ships when stated as a rule or corrected twice; one case is kept, not promoted. Fix with the lowest rung that holds: move an existing rule to its step, a line in the owning skill, a fact, a verifier check, a nudge hook, a blocking hook. When a fix doesn't hold, move it up a rung. | Feedback memories get written on one correction, and nothing tracks whether a fix held. |
| 8 | A budget for always-loaded text | A word budget for the rules every session loads. | CLAUDE.md plus MEMORY.md is 3,186 words in every thread, about Jarvis's own budget. The memory audit doesn't measure it. |

## Maybe later

- **Fewer asks over time:** after N plain approvals of one kind of ask with no
  correction, propose dropping that ask. Any miss restores it. Sends,
  deletes and prod changes never drop. ox's journal already records the
  approvals.
- **Replay cases for skills:** each real miss becomes a pinned case with a
  yes/no answer graded by a script. review-pr's past rounds are the seed.
- **A blind read of each ask:** before an ask reaches him, a reader sees only
  its text and names up to three gaps.

## Don't copy

- Model sessions that poll on schedules to run the system. Jarvis has six
  or so, and they cost Arun's attention when they misfire. ox keeps this work
  in Go.
- Several "what needs me" surfaces that overlap. ox has one: the asks queue,
  shown on the board.
- Dozens of hooks with their own shell parsers. Jarvis has five parsers, and
  one guard was patched five times before a file-permission lock replaced it.
- Long skills that pile up dated patches. One skill is 268 lines.
- Hardcoded home paths. Jarvis's hook dispatcher is a symlink to Arun's Mac,
  so in a clone every guard is silently off.
- Its vocabulary (flights, gates, towers, stations).
