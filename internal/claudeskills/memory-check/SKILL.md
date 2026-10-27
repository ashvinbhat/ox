---
name: memory-check
description: Walk Ashvin through the memory audit (`ox memory audit`) — Claude memories that are stale, point at gone paths, code or ox commands, drift from MEMORY.md, or sit stranded in other projects' stores — five at a time, applying only the fixes he approves. Use when he says "memory check", "clean up memory", "are my memories stale?", or asks about the memory line in the thread brief.
metadata:
  owner: ox
---

# Memory check

The Claude Code auto-memory store is the one store. `ox memory audit` finds
what may be wrong in it; you check each finding, propose a fix, and change
nothing he hasn't approved.

## 1. Run the audit

```bash
ox memory audit --json
```

`dir` is the store. `repos` are the repo folders searched for code (clones at
`~/.ox/repos/<name>`, plus the repo of any open thread). Each suggestion has an
`id`, `kind`, `file`, `subject` and `detail`. Stranded files carry an absolute
path.

## 2. Check every suggestion before you show it

Read each flagged memory file. Then, by kind:

- **gone-code**: decide whether the name belongs to the repos at all. Names
  from the JDK, Spring, Mongo, AWS, GCP, Maven, gh, an MCP tool, a database
  table, an app setting, or an example placeholder are external: they were
  never in the repos. For a name that does look like repo code, fetch and grep
  the likely repos before calling it gone:
  `git -C <repo> fetch -q origin && git -C <repo> grep -n -w -F <name> origin/HEAD --`.
  If it's gone, look for what replaced it (a rename, a moved file).
- **gone-path**: check whether it moved (list the parent dir) or was removed on
  purpose (a pruned worktree, a retired hook).
- **gone-command**: `ox --help` for what replaced it, if anything.
- **gone-cite**: same as gone-code or gone-path, for the cited entry.
- **stale**: read the memory's claims and check them against today: PR states
  (`gh pr view`), yoke tasks, files, dates. Say what still holds and what moved.
- **stranded**: compare with the store. It's a duplicate, still worth having
  (move it), or obsolete (delete it).
- **index**: fix mechanically: add the missing line, drop the dead link, or
  trim MEMORY.md back under 200 lines.

## 3. Propose, a batch at a time

First, if any gone-code names are external, list them all in one block, one
line each (`name — what it is`), and ask one question: keep them all?

Then the rest in batches of five. One line of finding, one of evidence, one
of proposal:

```
1. project_ox_harness_v2.md — `ox morph` and `ox ship` are gone
   Evidence: neither is in `ox --help`; missions froze in 1.0.
   Proposal: edit. "Run `ox ship`" → "Ship by PR (see CLAUDE.md)".
```

Proposals are one of: **edit** (show the exact old → new text), **delete** (the
memory no longer holds), **merge** (into which file, what moves), **move** (a
stranded memory into the store), **verify** (still true as written), **keep**
(fine as is; the suggestion never comes back).

End each batch with one question: which to apply. He may answer "all", "1, 3",
or change a proposal ("2: say X instead").

## 4. Apply only what he approved

- **edit / verify**: change the memory file. Set `metadata.verified` to today's
  date (`verified: YYYY-MM-DD`) on every memory he fixed or confirmed; that
  resets the 30-day stale clock.
- When a memory's truth rests on specific code or files, add
  `metadata.cites` so the next audit checks exactly those:
  `cites: ["backend:SegmentService", "ox:internal/harness/ticket.go", "~/.ox/secrets.env"]`.
- **delete**: remove the file and its MEMORY.md line.
- **merge**: fold the content into the target, delete the source, fix
  MEMORY.md.
- **move**: write the file into the store with the store's frontmatter (name,
  description, metadata.type), add its MEMORY.md line under the right heading,
  then delete the original.
- **keep**: `ox memory audit --keep <id> --note "<why>"` (repeat `--keep` for
  several). A stale memory can't be kept; verify it instead.

Then show the next batch. Anything he skips stays for the next audit.

## 5. Close

Run `ox memory audit` and report in one line: fixed, verified, moved, deleted,
kept, left.

## Rules

- Never change, delete or move a memory he didn't approve in this thread.
- Write memories the way he writes: short declarative sentences, facts first.
- Keep MEMORY.md one line per memory and under 200 lines.
- Don't save a memory about running the check.
