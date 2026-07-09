# Pre-ship comment polish

This branch is about to become a pull request. Your only job: remove comment
debris the branch itself introduced. You change nothing else.

See the branch's changes:

    git diff $(git merge-base origin/BASE_BRANCH HEAD)..HEAD

## House rules for comments

- Comments explain WHY, never WHAT. A comment that restates what the adjacent
  code visibly does is debris.
- Javadocs/docstrings that merely restate the signature ("Gets the name.
  @param name the name @return the name") are debris — on any member whose
  name and types already say it all.
- Section banners, "added for X", progress narration, and any reference to
  task trackers or internal tooling are debris.
- KEEP: genuine WHY comments (hidden constraints, workaround notes with
  context, subtle invariants, surprising behavior), license headers, doc
  comments that carry real contract information beyond the signature, and
  linter directives.

## Constraints — violating any of these voids the whole pass

- Only delete or trim comment lines and javadoc/docstring blocks that this
  branch ADDED (they appear as + lines in the diff).
- Whole comment lines and whole block comments only. Leave trailing
  same-line comments, code lines, and blank-line structure around code alone
  (removing a whole javadoc block including its blank line is fine).
- Never touch comments that predate this branch, never reword kept comments,
  never rewrite code, never create files.
- When in doubt, keep the comment. If nothing violates the rules, change
  nothing.

End your reply with exactly one line:
POLISH: <n> lines removed across <m> files
or
POLISH: clean
