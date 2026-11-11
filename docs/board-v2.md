# Fewer stops, a board that shows the work (2026-11-11)

All three steps built on 2026-11-11. Defaults taken: the stop list below, and
in-progress work grouped by ticket. An item's whole text opens its thread
(Ashvin, 2026-11-11), so there's no Open button.

## Why

- Threads stop and ask too often. This thread since Friday: an own-it
  offer, "strip the code too?", "draft a Slack note?", "start the rethink?".
  None needed Ashvin.
- ox's own rules add stops: an own-it offer before code on own tracks,
  findings that wait for his go, "ask before any design or architecture",
  tidy waiting for his OK, offers that end in "want me to…?".
- Nothing checks a question before it reaches him.
- The board says "Nothing needs you" while 3 threads wait on him: × hid them
  and they never came back.
- The board shows no work in progress. Tickets, PRs in review and each
  thread's work sit in yoke and the brief, where he doesn't look.

## What Jarvis does

Source: CBX1/jarvis-ai-assistant (`arun-main-v3`) and CBX1/desk.

- Asks Arun only for a send, a delete or other step that can't be undone, a
  choice he hasn't ruled on, or a fact only he knows. A hook sends every
  question back once with that list; the same words again pass
  (`PreToolUse/44-ask-rubric.sh`). No model involved.
- A desk page: a card per decision (the call, the pick, Go, Park, Comment),
  every session working a task with its state, queued work, goals.
- Sessions start on their own when there's room.

## 1. A stop list, and a check that holds it

Threads stop only for:

1. **Send**: anything that goes out as Ashvin (Slack, GitHub comments and
   reviews, email, Notion). He sees the exact text.
2. **Delete**: a delete, or a step that can't be undone (force-push to a
   shared branch, prod data or config).
3. **Merge**: merging to main, unless he already said go for that PR.
4. **Call**: a choice he hasn't made that changes direction (product
   behaviour, an API or data model other teams use).
5. **Fact**: something only he knows.

Everything else: do it, then say what was done and how to undo it.

- The list lives in `~/.ox/stops.md`. He changes it by telling any thread.
- A second hook, `ox hook ask`, runs before AskUserQuestion and before a
  next-step box with a question. A question that starts with a stop kind
  ("Send: …", "Delete: …") passes. Any other is sent back once with the
  list. Deterministic, no model.
- Each pass and send-back is logged. The brief shows "Stops today: 2 (5 sent
  back)".
- ox's rules lose their own stops:
  - own-it runs only when he asks for it;
  - findings are tasks threads pick up, not items waiting for his go;
  - design and architecture choices are made, logged in the journal and
    named in the reply, unless they're a Call;
  - offers become "done; here's how to undo it";
  - tidy archives and reports.

## 2. The board: needs you, then in progress

```
🐂 ox

NEEDS YOU · 2
Send · Landing migration: one union PGC per unit        7d  ×
  Reply to Neeraj on the redesign
Delete · Durable chat workflow failures                 7d  ×
  Remove 59 leaked QA segments
2 snoozed

IN PROGRESS · 5
CB-20605 Land user in pre-seeded onboarding
  frontend#10019 · in review · 2h
CB-20383 Don't drop an approval answer
  frontend#10074 · approved, tests running · 45h
Move client-driven background tasks to server     working · now
Download assets from LinkedIn Ad Manager          working · 3h
Backend/worker bundle split                       parked · 14d

Landed today: 2 PRs merged · 1 ticket done
```

The titles and PRs are today's; the two asks under Needs you are made up.

- **Needs you**: only stops, each with its kind; clicking it opens the
  thread. He answers in the thread. A thread can't take a message from the
  panel as his approval, so there's no Go button.
- × snoozes an item for a day. Snoozed items stay counted, so nothing
  waiting is hidden for good.
- **In progress**: one line per ticket, or per thread with no ticket: its PR,
  state and last move. Built from the work list ox already keeps.
- **Landed today**: what merged and what closed.

## Not taking

- Go buttons: an approval has to be his own words in the thread.
- Sessions that start on their own, goals, a phone page, the aviation
  words. Maybe later, once the stop list holds.

## Build

1. The stop list, `ox hook ask`, and ox's rules trimmed.
2. Board: needs you is stops only; a click opens the thread; × snoozes.
3. Board: in progress and landed today.

After a week: stops per day, and how many were sent back.

Opening a thread: a pane draws only http(s) links, so each item links to a
stand-in address the panel answers by running `open
claude://claude.ai/epitaxy/<thread>`, the link Jarvis uses.

## Changing it

The stop list is `~/.ox/stops.md`: tell any thread to add or drop a stop.
