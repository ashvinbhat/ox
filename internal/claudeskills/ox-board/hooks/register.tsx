import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { Ask, Board, Thread } from '../types'

const PANE = 'ox-board'
const TITLE = 'ox'
const EVERY_MS = 60_000
const board = atom({ plugin: 'ox-board', key: 'board' } as const, null)

// The desktop app's PATH may not reach ~/go/bin, where make install puts ox.
const OX_NOW = ['/bin/sh', '-c', 'exec "$(command -v ox || echo "$HOME/go/bin/ox")" now --brief --json']

type ViewAsk = { id: string; what: string; where?: string; link?: string; since: string }
type View = {
  at: string
  threads?: { sessionId: string; title: string; track?: string; state: string; next?: string; archivable?: boolean }[]
  asks?: { waiting?: ViewAsk[]; fixes?: ViewAsk[] }
}

function age(since: string, now: number): string {
  const minutes = Math.max(0, Math.round((now - Date.parse(since)) / 60_000))
  if (minutes < 60) return `${minutes}m`
  if (minutes < 48 * 60) return `${Math.round(minutes / 60)}h`
  return `${Math.round(minutes / 1440)}d`
}

async function refresh($: EngineInterface): Promise<void> {
  const ran = await $.process
    .run(OX_NOW, { timeoutMs: 20_000 })
    .catch(err => ({ exitCode: -1, stdout: '', stderr: String(err) }))
  const now = await $.clock.now()
  const fail = (error: string) =>
    update($, board, b => (b ? { ...b, error } : { at: now, waiting: [], fixes: [], threads: [], archivable: 0, error }))
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
  const ask = (a: ViewAsk): Ask => ({ id: a.id, what: a.what, where: a.where, link: a.link, age: age(a.since, now) })
  const threads = view.threads ?? []
  const next: Board = {
    at: now,
    waiting: (view.asks?.waiting ?? []).map(ask),
    fixes: (view.asks?.fixes ?? []).map(ask),
    threads: threads.map((t): Thread => ({ id: t.sessionId, title: t.title, track: t.track, state: t.state, next: t.next })),
    archivable: threads.filter(t => t.archivable).length,
  }
  await update($, board, () => next)
}

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
    await $.command.register({ name: 'ox-board', description: 'Show the ox board: what waits on you and where threads stand' })
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

  on('ui.render', { component: 'Pane', requestId: PANE }, async ($, e) => {
    const { Box, Text, Link, Button } = $.ui.resolve(e)
    const b = await read($, board)
    if (!b) return <Text dimColor>Reading ox…</Text>

    const row = (a: Ask) => (
      <Box key={a.id} flexDirection="column" marginBottom={1}>
        {a.link?.startsWith('https://') ? (
          <Link href={a.link}>{a.what}</Link>
        ) : (
          <Text wrap="wrap">{a.what}</Text>
        )}
        <Text dimColor wrap="truncate-end">
          {[a.where, a.age].filter(Boolean).join(' · ')}
        </Text>
      </Box>
    )
    const section = (title: string, list: Ask[], max: number) => (
      <Box flexDirection="column" marginBottom={1}>
        <Text bold>
          {title} ({list.length})
        </Text>
        {list.length === 0 && <Text dimColor>Nothing.</Text>}
        {list.slice(0, max).map(row)}
        {list.length > max && <Text dimColor>+{list.length - max} more</Text>}
      </Box>
    )
    const inState = (state: string) => b.threads.filter(t => t.state === state)
    const counts = ['working', 'stuck', 'in review', 'done']
      .map(s => `${s} ${inState(s).length}`)
      .join(' · ')
    const active = [...inState('stuck'), ...inState('working')]
    const parked = inState('parked')

    return (
      <Box flexDirection="column" width={e.props.bodyColumns}>
        {section('Waiting on you', b.waiting, 6)}
        {section('Threads can fix', b.fixes, 4)}
        <Box flexDirection="column" marginBottom={1}>
          <Text bold>Threads</Text>
          <Text dimColor>{counts}</Text>
          {active.map(t => (
            <Text key={t.id} wrap="truncate-end">
              {t.state === 'stuck' ? 'stuck ' : ''}
              {t.title}
              <Text dimColor>{t.track ? ` · ${t.track}` : ''}</Text>
            </Text>
          ))}
          {parked.length > 0 && <Text bold>Parked ({parked.length})</Text>}
          {parked.map(t => (
            <Box key={t.id} flexDirection="column">
              <Text wrap="truncate-end">{t.title}</Text>
              <Text dimColor wrap="truncate-end">
                next: {t.next}
              </Text>
            </Box>
          ))}
          {b.archivable > 0 && (
            <Text dimColor>
              {b.archivable} done {b.archivable === 1 ? 'thread' : 'threads'} can be archived
            </Text>
          )}
        </Box>
        {b.error && <Text color="red">ox: {b.error}</Text>}
        <Box flexDirection="row" gap={1}>
          <Button key="refresh" label="Refresh" onPress={() => void refresh($)} />
        </Box>
      </Box>
    )
  })
}
