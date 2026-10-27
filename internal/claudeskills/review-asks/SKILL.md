---
name: review-asks
description: Find the PR reviews people asked Ashvin for in Slack (DMs, group DMs, @-mentions) and record them in the ox review ledger, then show his review queue. Use when he asks "any review asks?", "who wants a review?", "what should I review?", or plans his day.
metadata:
  owner: ox
---

# Review asks

Collect the reviews people asked Ashvin for and record each with `ox review ask`.
Only explicit asks count. Never add PRs from GitHub team review requests, and
never suggest PRs nobody asked him about.

## 1. Search Slack

Use the Slack connector's `slack_search_public_and_private`. Get his user id
from `slack_read_user_profile` (no arguments) if you don't have it. The window
is the last 7 days unless he names another.

Run both searches with `response_format: "detailed"`, `include_context: false`,
`sort: "timestamp"`, `limit: 20`. Follow the cursor until results are older
than the window, at most 3 pages each.

1. DMs and group DMs: `keywords: ["pull"]`, `filters: "is:dm has:link after:YYYY-MM-DD"`, `channel_types: "im,mpim"`.
2. Mentions: `keywords: ["<@HIS_ID>", "pull"]`, `filters: "after:YYYY-MM-DD"`.

## 2. Keep only real asks

A message is an ask when all of these hold:

- It links `github.com/<owner>/<repo>/pull/<n>`.
- Someone else wrote it. His own messages are him asking others. Bots
  (Autobot, Mycroft) don't count.
- It is a DM or group DM he is in, or its text contains `<@HIS_ID>`. The
  mention search also returns messages that tag other people; drop those.
- It asks for a review, re-review or approval: "review", "PTAL", "take a
  look", "approve again", or a bare PR link sent to him in a DM. A status note
  about his own PR ("check-title is failing on …") is not an ask.

Then check each PR with `gh pr view <url> --json author,state`. Drop PRs he
authored and PRs already merged or closed.

## 3. Record each ask

```bash
ox review ask <pr-url> --from "<sender name>" --where slack-dm --link "<permalink>" --note "<their words, one line>" --at "<message time, RFC3339>"
```

Use `--where slack-thread` for channel messages. Recording is idempotent per
permalink, so re-running is safe.

## 4. Show the queue

Run `ox reviews` and show it as is. Lead with the oldest "to review" item and
how long it has waited. Offer to start that one with `/review-pr`. Don't start
a review he didn't pick.
