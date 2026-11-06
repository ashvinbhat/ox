---
name: daily-archive
description: The daily routine that archives Ashvin's finished desktop threads on its own, then says what it archived. Archives exactly the threads `ox work archivable` lists, records them with `ox work archived`, and ends with the list. Runs from the "ox daily archive" scheduled task; use when that task fires or he says "run the daily archive".
metadata:
  owner: ox
---

# Daily archive

Ashvin decided on 2026-11-05 that this routine archives threads on its own
and then tells him what it archived. That decision covers only the threads
`ox work archivable` lists today. Archive nothing else, and ask nothing.

1. Run `ox work refresh` so tickets and work are current, then
   `ox work archivable --json`. It lists threads whose last turn finished with
   nothing waiting on him, that aren't parked, have no open PR, have all
   their yoke work done or dropped, have a clean and pushed worktree, and
   have been quiet for 24 hours.
2. Archive each one with the session-management archive tool, passing its
   `id` and the reason "daily archive: finished, quiet 24h, work done". The
   app refuses a thread that's pinned, open on screen or still working; leave
   those, and don't retry.
3. Run `ox work archived <id>...` with the ids that did archive, so the next
   brief says so.
4. End with one line per archived thread (its title), then one line per
   thread the app refused and why. If the list was empty, say "Nothing to
   archive today." Nothing else: no offers, no questions.

Pending findings don't keep a thread open; they live in yoke.
