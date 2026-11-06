import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { Board, Item } from '../types'

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
  threads?: { sessionId: string; title: string; state: string; lastActive: string; dropped?: boolean }[]
  asks?: { waiting?: { id: string; kind: string; what: string; where?: string; thread?: string; since: string; settled?: string }[] }
  findings?: { id: string; title: string; count: number; since: string; dropped?: boolean }[]
}

function age(since: string, now: number): string {
  const minutes = Math.max(0, Math.round((now - Date.parse(since)) / 60_000))
  if (minutes < 60) return `${minutes}m`
  if (minutes < 48 * 60) return `${Math.round(minutes / 60)}h`
  return `${Math.round(minutes / 1440)}d`
}

// The threads that need Ashvin: a question he hasn't dropped, a PR of theirs
// ready to merge, or a turn that went silent. Asks that aren't a thread's
// (reviews, memory, recap) and fixes threads can make stay off the panel.
// Below them, one line per ticket with findings waiting for his go.
function attention(view: View, now: number): Item[] {
  const items: Item[] = []
  for (const a of view.asks?.waiting ?? []) {
    if (!a.thread || !a.where) continue
    if (a.kind === 'question') {
      const waiting = a.settled ? `${a.what} (${a.settled}; still needed?)` : a.what
      items.push({ id: a.id, title: a.where, waiting, age: age(a.since, now) })
    }
    if (a.kind === 'merge') {
      const ref = a.what.replace(/^Merge /, '').split(':')[0]
      items.push({ id: a.id, title: a.where, waiting: `${ref} is ready to merge`, age: age(a.since, now) })
    }
  }
  for (const t of view.threads ?? []) {
    if (t.state === 'stuck' && !t.dropped) {
      items.push({ id: `stuck:${t.sessionId}`, title: t.title, waiting: 'Went quiet mid-turn; it may have stopped', age: age(t.lastActive, now) })
    }
  }
  for (const f of view.findings ?? []) {
    if (f.dropped) continue
    const waiting = `${f.count} ${f.count === 1 ? 'finding' : 'findings'} waiting for your go`
    items.push({ id: f.id, title: f.title, waiting, age: age(f.since, now) })
  }
  return items
}

async function refresh($: EngineInterface): Promise<void> {
  // PR state is cached; this fetches it once it's 10 minutes old, so a merged
  // PR drops off. Only one panel's call does the fetch.
  void $.process.run(ox('asks', 'refresh', '--if-due'), { timeoutMs: 60_000 }).catch(() => undefined)
  const ran = await $.process
    .run(ox('now', '--brief', '--json'), { timeoutMs: 20_000 })
    .catch(err => ({ exitCode: -1, stdout: '', stderr: String(err) }))
  const now = await $.clock.now()
  const fail = (error: string) => update($, board, b => (b ? { ...b, error } : { at: now, items: [], error }))
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
  await update($, board, () => ({ at: now, items: attention(view, now), problems: view.problems ?? [] }))
}

// Clear drops items through ox, so every panel loses them. Each comes back
// when its thread asks something new or moves again, or a new finding
// arrives under its ticket.
async function clear($: EngineInterface, ids: string[]): Promise<void> {
  const ran = await $.process
    .run(ox('asks', 'drop', ...ids), { timeoutMs: 20_000 })
    .catch(err => ({ exitCode: -1, stdout: '', stderr: String(err) }))
  if (ran.exitCode !== 0) {
    const error = ran.stderr.trim().split('\n')[0] || `ox exited ${ran.exitCode}`
    await update($, board, b => (b ? { ...b, error } : b))
    return
  }
  $.ui.toast(ids.length === 1 ? 'Cleared. It comes back when it changes.' : `Cleared ${ids.length}. Each comes back when it changes.`)
  await refresh($)
}

const escape = (s: string) => s.replace(/([\\`*_[\]<>#|])/g, '\\$1')

const line = (i: Item) => `**${escape(i.title)}**  \n${escape(i.waiting)} · ${i.age}`

// A job that keeps failing (gh logged out, say) leaves the board quietly stale,
// so it says so under the items until the job succeeds again.
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
    await $.command.register({ name: 'ox-board', description: 'Show the threads that need you' })
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
  on('ui.render', { component: 'Pane', requestId: PANE }, async ($, e) => {
    const { Box, Button, Markdown, Text } = $.ui.resolve(e)
    const b = await read($, board)
    if (!b) return <Text dimColor>Reading ox…</Text>
    if (b.items.length === 0) {
      return <Markdown key="threads" text={['Nothing needs you right now.', note(b)].filter(Boolean).join('\n\n')} />
    }
    const rows = b.items.map(i => (
      <Box key={i.id} flexDirection="row" gap={1}>
        <Box flexDirection="column" flexGrow={1} flexShrink={1}>
          <Markdown key={`${i.id}:text`} text={line(i)} />
        </Box>
        <Button key={`${i.id}:clear`} label="×" plain dimColor onPress={() => void clear($, [i.id])} />
      </Box>
    ))
    const ids = b.items.map(i => i.id)
    return (
      <Box flexDirection="column" gap={1}>
        {rows}
        {note(b) ? <Markdown key="notes" text={note(b)} /> : []}
        <Box flexDirection="row">
          <Button key="clear" label="Clear all" dimColor onPress={() => void clear($, ids)} />
        </Box>
      </Box>
    )
  })
}
