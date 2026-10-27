---
name: own-it
description: Run an own-it round on the task in this thread — Ashvin predicts the approach before any code is written, Claude builds it, then compares his prediction with what was built and explains each gap. Use when he says "own it", "let me own this", "/own-it", or accepts an own-it offer on an own track (durable, ox).
metadata:
  owner: ox
---

# Own it

Ashvin wants to understand the work, not just accept diffs. He predicts the
approach, you build it, then you show where his prediction and the build
differ and why. Learning holds when he generates first and then asks why.

## 1. Before any code: ask for his prediction

State the task in two lines: what has to change and the constraint that
makes it non-trivial. Then ask one question and stop:

> Your call: where does the change go, what shape does it take, and what's
> the main risk? A line each is enough. ("skip" runs the task normally.)

Don't hint at your own plan. Don't list options; that gives the answer away.
If he says "skip", do the task normally and log nothing.

## 2. Build it

Do the work the way you would have. Don't bend the design toward his
prediction. If his prediction is better than your plan, say so, build his,
and note it in the comparison.

## 3. Compare

After the work is done, before the closing message:

```
Your call vs what I built
- Where: match. Both in ChatTurnWorkflow.
- Shape: gap. You said a signal per chunk; I used one Update per call.
  Why: a signal can't return a value, and replay needs the call's result in history.
- Risk: match. Both flagged the 2 MB history cap.
```

One line per point, then the why for each gap in at most two lines, pointing
at the code (file:line). Grade the round: **match** (all three), **partial**,
or **miss** (where or shape wrong).

## 4. Log it

```bash
ox journal own-it --grade partial \
  --predicted "signal per chunk from the Activity" \
  --actual "one Update per call; the Workflow holds results" \
  --gap "signals can't return values; replay needs results in history"
```

## When to offer it

On an own track (`own: true` in `~/.ox/tracks.yaml`), offer a round once,
before writing code, if `ox journal status` shows rounds left today (2 a day).
One line: "Own-it round on this? You predict, I build, then we compare."
Don't offer it for one-line fixes, pure investigation, or when he's in a
hurry. When he asks for it, run it whatever the count.
