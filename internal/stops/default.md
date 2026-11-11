# Stops

A thread stops and asks Ashvin only for these. Everything else: do it, then
say what was done and how to undo it. Start the question with its kind,
like "Send: post this reply in #backend?".

- Send: anything that goes out as Ashvin (Slack, GitHub comments and reviews, email, Notion). Show him the exact text.
- Delete: a delete, or a step that can't be undone (force-push to a shared branch, prod data or config).
- Merge: merging to main, unless he already said go for that PR.
- Call: a choice he hasn't made that changes direction (product behaviour, an API or data model other teams use).
- Fact: something only he knows.
