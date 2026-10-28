import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { Ask, Board, Thread, Track } from '../types'

const PANE = 'ox-board'
const TITLE = 'ox'
// Every open panel reads ox this often, so a change made in one thread (an
// answer, a dropped ask) shows in the others within seconds.
const EVERY_MS = 15_000
const board = atom({ plugin: 'ox-board', key: 'board' } as const, null)
const replyTo = atom({ plugin: 'ox-board', key: 'replyTo' } as const, null)

// The desktop app's PATH may not reach ~/go/bin, where make install puts ox.
const ox = (...args: string[]) => ['/bin/sh', '-c', 'exec "$(command -v ox || echo "$HOME/go/bin/ox")" "$@"', 'ox', ...args]

type ViewAsk = Omit<Ask, 'age'> & { since: string }
type View = {
  threads?: { sessionId: string; title: string; track?: string; state: string; next?: string }[]
  asks?: { waiting?: ViewAsk[]; fixes?: ViewAsk[] }
  tracks?: Track[]
}

const STATES = ['working', 'stuck', 'in review', 'parked', 'done']

function age(since: string, now: number): string {
  const minutes = Math.max(0, Math.round((now - Date.parse(since)) / 60_000))
  if (minutes < 60) return `${minutes}m`
  if (minutes < 48 * 60) return `${Math.round(minutes / 60)}h`
  return `${Math.round(minutes / 1440)}d`
}

async function refresh($: EngineInterface): Promise<void> {
  // PR state is cached; this fetches it when it's 10 minutes old, and only
  // one panel's call does the fetch.
  void $.process.run(ox('asks', 'refresh', '--if-due'), { timeoutMs: 60_000 }).catch(() => undefined)
  const ran = await $.process
    .run(ox('now', '--brief', '--json'), { timeoutMs: 20_000 })
    .catch(err => ({ exitCode: -1, stdout: '', stderr: String(err) }))
  const now = await $.clock.now()
  const fail = (error: string) =>
    update($, board, b => (b ? { ...b, error } : { at: now, waiting: [], fixes: [], threads: [], tracks: [], error }))
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
  const ask = ({ since, ...a }: ViewAsk): Ask => ({ ...a, age: age(since, now) })
  const next: Board = {
    at: now,
    waiting: (view.asks?.waiting ?? []).map(ask),
    fixes: (view.asks?.fixes ?? []).map(ask),
    threads: (view.threads ?? []).map((t): Thread => ({ id: t.sessionId, title: t.title, track: t.track, state: t.state, next: t.next })),
    tracks: view.tracks ?? [],
  }
  await update($, board, () => next)
}

async function sendTo($: EngineInterface, a: Ask, text: string): Promise<void> {
  if (!a.thread) return
  const sent = await $.session
    .send({ to: { sessionId: a.thread }, text })
    .catch(err => ({ isDelivered: false as const, reason: String(err) }))
  $.ui.toast(sent.isDelivered ? `Sent to ${a.where ?? 'its thread'}` : `Not sent: ${sent.reason}`)
}

async function askHere($: EngineInterface, text: string): Promise<void> {
  await $.prompt.submit({ text, asUser: true })
}

function fixText(a: Ask): string {
  return (
    `Fix ${a.what.replace(/ \(draft\)$/, '')}: ${a.link}. Check first whether our change caused it. ` +
    'Resolve conflicts without breaking main; ask me before any design, big or breaking change.'
  )
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
    const els = $.ui.resolve(e)
    const { Box, Text, Link, Button } = els
    // The mobile app draws no text field, so replies need another surface.
    const Input = 'Input' in els ? els.Input : null
    const b = await read($, board)
    if (!b) return <Text dimColor>Reading ox…</Text>
    const replying = await read($, replyTo)
    const mine = b.waiting.filter(a => a.kind !== 'memory' && a.kind !== 'recap')

    const trackLine = (key: string, name: string, forYou: number, threads: Thread[]) => {
      const rest = STATES.map(s => [s, threads.filter(t => t.state === s).length] as const)
        .filter(([, n]) => n > 0)
        .map(([s, n]) => `${n} ${s}`)
      return (
        <Box key={key} flexDirection="row" justifyContent="space-between">
          <Text wrap="truncate-end">{name}</Text>
          <Text>
            {forYou > 0 && <Text color="warning">{forYou} for you</Text>}
            <Text dimColor>{(forYou > 0 && rest.length > 0 ? ' · ' : '') + rest.join(' · ')}</Text>
          </Text>
        </Box>
      )
    }
    const tracks = [...b.tracks, { short: undefined, name: 'No track' }]
      .map(t => ({
        ...t,
        forYou: mine.filter(a => a.track === t.short).length,
        threads: b.threads.filter(th => th.track === t.short && th.state !== 'needs you'),
      }))
      .filter(t => t.forYou + t.threads.length > 0)

    const action = (a: Ask) => {
      switch (a.kind) {
        case 'question':
          return Input ? (
            <Button key={`${a.id}:reply`} label="Reply" onPress={() => void update($, replyTo, r => (r === a.id ? null : a.id))} />
          ) : null
        case 'review':
          return <Button key={`${a.id}:start`} label="Review" onPress={() => void askHere($, `Review ${a.link}`)} />
        case 'memory':
          return <Button key={`${a.id}:start`} label="Start" onPress={() => void askHere($, 'check my memory')} />
        case 'recap':
          return <Button key={`${a.id}:start`} label="Start" onPress={() => void askHere($, 'recap')} />
        case 'fix':
          return a.thread ? (
            <Button key={`${a.id}:fix`} label="Fix" onPress={() => void sendTo($, a, fixText(a))} />
          ) : (
            <Button key={`${a.id}:fix`} label="Fix" onPress={() => void askHere($, fixText(a))} />
          )
        default:
          return null
      }
    }
    const item = (a: Ask) => {
      const what = a.what.replace(/\s*\(\/[a-z-]+\)$/, '')
      const meta = [a.where, a.age].filter(Boolean).join(' · ')
      return (
        <Box key={a.id} flexDirection="column" marginBottom={1}>
          <Box flexDirection="row" justifyContent="space-between" gap={1}>
            <Box flexDirection="column" flexShrink={1}>
              {a.link?.startsWith('https://') ? <Link href={a.link} label={what} /> : <Text wrap="wrap">{what}</Text>}
              <Text dimColor wrap="truncate-end">
                {meta}
              </Text>
            </Box>
            {action(a)}
          </Box>
          {replying === a.id && Input && (
            <Input
              key={`${a.id}:answer`}
              placeholder="Your answer"
              submitLabel="Send"
              autoFocus
              onSubmit={(text: string) => {
                void update($, replyTo, () => null)
                if (text.trim()) void sendTo($, a, text)
              }}
            />
          )}
        </Box>
      )
    }
    const section = (title: string, asks: Ask[]) =>
      asks.length > 0 && (
        <Box flexDirection="column" marginTop={1}>
          <Text bold>{title}</Text>
          {asks.map(item)}
        </Box>
      )
    const parked = b.threads.filter(t => t.state === 'parked')

    return (
      <Box flexDirection="column" width={e.props.bodyColumns}>
        {b.error && <Text color="error">ox: {b.error}</Text>}
        {tracks.map(t => trackLine(t.short ?? 'none', t.name, t.forYou, t.threads))}
        {section('For you', b.waiting)}
        {section('PRs to fix', b.fixes)}
        {b.waiting.length + b.fixes.length === 0 && <Text dimColor>Nothing needs you.</Text>}
        {parked.length > 0 && (
          <Box flexDirection="column" marginTop={1}>
            <Text bold>Parked</Text>
            {parked.map(t => (
              <Box key={t.id} flexDirection="column" marginBottom={1}>
                <Text wrap="truncate-end">{t.title}</Text>
                <Text dimColor wrap="truncate-end">
                  Next: {t.next}
                </Text>
              </Box>
            ))}
          </Box>
        )}
      </Box>
    )
  })
}
