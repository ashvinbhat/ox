## Output hygiene — CRITICAL

The workspace tools (ox / yoke / missions / checkpoints / learnings) are PRIVATE to this
environment. Never mention them in anything that lands in the code repository or in
anything a human collaborator sees on GitHub:

- Commit messages: describe the change, not the tooling. No "shipped via ox", no
  "closes yoke task #N", no "checkpoint saved". Use the actual issue tracker
  (Jira / Notion / Linear) if one is referenced in the task.
- PR titles and bodies: same rule. The reviewer should not learn what tools you
  used; they should read what changed and why.
- Code comments / test names / assertion messages / TODO markers: never reference
  ox, yoke, missions, task IDs like "Task #123", or the local workspace path.
- Files added to the repo: absolutely no ox/yoke/mission references.

You may (and should) use the internal tools to track your own progress. That's what
they exist for. Just don't advertise them outward.

## Comments — WRITE ALMOST NONE (non-negotiable)

Default: no comments. No JSDoc / Javadoc / KDoc. No "this function returns X",
no narrating the code line-by-line.

Write a comment ONLY when it explains WHY — and only when the code cannot
explain itself:

- A non-obvious constraint or invariant.
- A workaround for a specific bug (link the bug).
- Behavior that would surprise a careful reader ("looks wrong but is intentional
  because X").
- A legal / regulatory / performance reason for an unusual structure.

Never write comments that:

- Describe WHAT the next line, function, class, or method does. Well-named
  identifiers already do that.
- Restate the code in prose. If the reader can read the code, they can read
  the code.
- Reference the current task, PR, or refactor ("added for task X", "part of
  the fold"). That context belongs in the PR description, not in the source.

If you feel a comment is needed to explain what the code does, that's a signal
the code needs better names / structure — fix that instead of adding the comment.
