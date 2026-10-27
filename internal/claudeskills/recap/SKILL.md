---
name: recap
description: Weekly recap of the decision journal — three concepts from the week's decisions, each with one explain-it-back prompt Ashvin answers, then the gaps filled and the result logged, plus his own-it prediction score. Use when he says "recap", "Friday recap", "what did I learn this week", or asks about the recap line in the thread brief.
metadata:
  owner: ox
---

# Recap

Three concepts from the week, explained back by Ashvin. The point is
retrieval: he explains, then you fill the gaps. Don't lecture first.

## 1. Read the week

```bash
ox journal --days 7
ox journal status
ox journal --days 14 | grep -A3 "Recap:"
```

The last command shows the previous recap's shaky concepts.

## 2. Pick three concepts

A concept is the idea behind one or more decisions: a mechanism, constraint
or trade-off he'd need to explain to a teammate. "Temporal replays a
Workflow from history, so its code must be deterministic" is a concept;
"we renamed recorder.ts" is not.

- At most one shaky concept from the last recap comes back.
- Prefer concepts from own tracks (`own: true` in `~/.ox/tracks.yaml`).
- Prefer decisions where an own-it round was a miss or a partial.

## 3. One prompt at a time

Ask a prompt that needs reasoning, not recall: "Why does…", "What breaks
if…", "When would you pick X over Y…". Name the decision it comes from (date,
thread). Then stop and wait.

After his answer, in at most four lines:

- **Solid** or **shaky**, in one word.
- What he got right, in one line.
- The missing piece, with the concrete case (file, PR or decision) that shows it.

Then the next prompt. If he says "skip", move on and count it as shaky.

## 4. Close

One line for the own-it score: "Own-it this week: 2 match, 1 partial, 1 miss"
from `ox journal status` (say "no rounds" if there were none). Then log:

```bash
ox journal recap --solid "Temporal replay determinism" \
  --shaky "Update vs Signal; ECS scale-in protection"
```

## Rules

- Three concepts, never more. The whole recap takes about ten minutes.
- Use only what's in the journal and the code it names. Don't invent decisions.
- Write the way he writes: short sentences, facts first.
