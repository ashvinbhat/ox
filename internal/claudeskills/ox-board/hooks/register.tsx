import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { Board, Need, Work } from '../types'

const PANE = 'ox-board'
const TITLE = '🐂 ox'
// Every open panel reads ox this often, so a change made in one thread shows
// in the others within seconds.
const EVERY_MS = 15_000
const board = atom({ plugin: 'ox-board', key: 'board' } as const, null)

// The desktop app's PATH may not reach ~/go/bin, where make install puts ox.
const ox = (...args: string[]) => ['/bin/sh', '-c', 'exec "$(command -v ox || echo "$HOME/go/bin/ox")" "$@"', 'ox', ...args]

type View = {
  problems?: string[]
  board?: { needs?: Need[]; snoozed?: number; inProgress?: Work[]; landed?: string[] }
}

function age(since: string, now: number): string {
  const minutes = Math.max(0, Math.round((now - Date.parse(since)) / 60_000))
  if (minutes < 60) return `${minutes}m`
  if (minutes < 48 * 60) return `${Math.round(minutes / 60)}h`
  return `${Math.round(minutes / 1440)}d`
}

async function refresh($: EngineInterface): Promise<void> {
  // PR state is cached; this fetches it once it's 10 minutes old, so a merged
  // PR drops off. Only one panel's call does the fetch.
  void $.process.run(ox('asks', 'refresh', '--if-due'), { timeoutMs: 60_000 }).catch(() => undefined)
  const ran = await $.process
    .run(ox('now', '--brief', '--json'), { timeoutMs: 20_000 })
    .catch(err => ({ exitCode: -1, stdout: '', stderr: String(err) }))
  const now = await $.clock.now()
  const fail = (error: string) =>
    update($, board, b => (b ? { ...b, error } : { at: now, needs: [], snoozed: 0, inProgress: [], landed: [], error }))
  if (ran.exitCode !== 0) {
    await fail(ran.stderr.trim().split('\n')[0] || `ox exited ${ran.exitCode}`)
    return
  }
  let view: View
  try {
    view = JSON.parse(ran.stdout) as View
  } catch {
    await fail("ox printed something that isn't JSON")
    return
  }
  const v = view.board ?? {}
  await update($, board, () => ({
    at: now,
    needs: v.needs ?? [],
    snoozed: v.snoozed ?? 0,
    inProgress: v.inProgress ?? [],
    landed: v.landed ?? [],
    problems: view.problems ?? [],
  }))
}

// × snoozes through ox, so every panel loses the item. It comes back after a
// day, or sooner when its thread asks something new.
async function snooze($: EngineInterface, ids: string[]): Promise<void> {
  const ran = await $.process
    .run(ox('asks', 'drop', ...ids), { timeoutMs: 20_000 })
    .catch(err => ({ exitCode: -1, stdout: '', stderr: String(err) }))
  if (ran.exitCode !== 0) {
    const error = ran.stderr.trim().split('\n')[0] || `ox exited ${ran.exitCode}`
    await update($, board, b => (b ? { ...b, error } : b))
    return
  }
  $.ui.toast(ids.length === 1 ? 'Snoozed for a day.' : `Snoozed ${ids.length} for a day.`)
  await refresh($)
}

// A pane draws only http(s) links, so each thread gets a stand-in address the
// panel answers itself by asking macOS to open the thread in the Claude app.
const href = (thread: string) => `https://ox.invalid/thread/${thread}`

async function openThread($: EngineInterface, thread: string): Promise<void> {
  if (!/^local_[0-9a-f-]+$/.test(thread)) return
  const ran = await $.process
    .run(['/usr/bin/open', `claude://claude.ai/epitaxy/${thread}`], { timeoutMs: 10_000 })
    .catch(err => ({ exitCode: -1, stdout: '', stderr: String(err) }))
  if (ran.exitCode !== 0) {
    const error = `couldn't open the thread: ${ran.stderr.trim().split('\n')[0] || `open exited ${ran.exitCode}`}`
    await update($, board, b => (b ? { ...b, error } : b))
  }
}

const escape = (s: string) => s.replace(/([\\`*_[\]<>#|])/g, '\\$1')

const link = (text: string, thread: string) => `[${text}](${href(thread)})`

const needText = (n: Need, now: number) =>
  `${link(`**${escape(n.kind ? `${n.kind} · ${n.title}` : n.title)}**`, n.thread)}  \n${link(`${escape(n.what)} · ${age(n.since, now)}`, n.thread)}`

const STATE: Record<string, string> = { stuck: 'went quiet mid-turn', 'in review': 'in review', working: 'working', parked: 'parked' }

const workText = (f: Work, now: number) => {
  const detail = [...(f.prs ?? []), STATE[f.state] ?? f.state, age(f.since, now)].join(' · ')
  const title = f.ticket ? escape(f.title) : `**${escape(f.title)}**`
  return `${link(title, f.thread)}  \n${link(escape(detail), f.thread)}`
}

// A job that keeps failing (gh logged out, say) leaves the board quietly stale,
// so it says so at the bottom until the job succeeds again.
const note = (b: Board) =>
  [...(b.problems ?? []), ...(b.error ? [b.error] : [])].map(t => `_ox: ${escape(t)}_`).join('\n\n')

// Polling starts with the first pane, so a terminal or headless session that
// never shows one never runs ox.
let polling = false

async function show($: EngineInterface): Promise<void> {
  if (!polling) {
    polling = true
    $.clock.every(EVERY_MS, () => void refresh($))
  }
  await $.ui.open({ id: PANE, title: TITLE })
  await refresh($)
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    await $.command.register({ name: 'ox-board', description: 'Show what needs you and what is in progress' })
    if ((await $.session.surfaces()).includes('desktop')) void show($)
    return next(e)
  })

  on('session.attach', { surface: 'desktop' }, async ($, e, next) => {
    void show($)
    return next(e)
  })

  on('command.run', { command: 'ox-board' }, async $ => {
    await show($)
    return { text: 'ox board opened.' }
  })

  // Markdown draws with the chat's own type, so the panel reads like a reply.
  // Every line of an item is a link into its thread.
  on('ui.render', { component: 'Pane', requestId: PANE }, async ($, e) => {
    const { Box, Button, Markdown, Text } = $.ui.resolve(e)
    const b = await read($, board)
    if (!b) return <Text dimColor>Reading ox…</Text>
    const now = b.at
    const needs = b.needs ?? []
    const working = b.inProgress ?? []
    const parts = []

    if (needs.length > 0) {
      parts.push(<Text key="needs:head" dimColor>{`NEEDS YOU · ${needs.length}`}</Text>)
      for (const n of needs) {
        parts.push(
          <Box key={n.id} flexDirection="row" gap={1}>
            <Box flexDirection="column" flexGrow={1} flexShrink={1}>
              <Markdown
                key={`need:${n.id}`}
                text={needText(n, now)}
                pressableLinks={[href(n.thread)]}
                onLinkPress={() => void openThread($, n.thread)}
              />
            </Box>
            <Button key={`${n.id}:snooze`} label="×" plain dimColor onPress={() => void snooze($, [n.id])} />
          </Box>,
        )
      }
    } else {
      parts.push(<Markdown key="needs:none" text="Nothing needs you right now." />)
    }
    if (b.snoozed > 0) parts.push(<Text key="snoozed" dimColor>{`${b.snoozed} snoozed`}</Text>)

    if (working.length > 0) {
      parts.push(<Text key="progress:head" dimColor>{`IN PROGRESS · ${working.length}`}</Text>)
      working.forEach((f, i) => {
        if (f.ticket && f.ticket !== working[i - 1]?.ticket) {
          parts.push(<Markdown key={`ticket:${f.ticket}`} text={`**${escape(f.ticket)}**`} />)
        }
        parts.push(
          <Markdown
            key={`work:${f.thread}`}
            text={workText(f, now)}
            pressableLinks={[href(f.thread)]}
            onLinkPress={() => void openThread($, f.thread)}
          />,
        )
      })
    }
    if ((b.landed ?? []).length > 0) {
      parts.push(<Text key="landed" dimColor>{`Landed today: ${b.landed.join(', ')}`}</Text>)
    }
    if (note(b)) parts.push(<Markdown key="notes" text={note(b)} />)
    if (needs.length > 0) {
      parts.push(
        <Box key="actions" flexDirection="row">
          <Button key="snooze-all" label="Snooze all" dimColor onPress={() => void snooze($, needs.map(n => n.id))} />
        </Box>,
      )
    }
    return (
      <Box flexDirection="column" gap={1}>
        {parts}
      </Box>
    )
  })
}
