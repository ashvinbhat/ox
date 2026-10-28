import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register, RenderChildren } from 'claude-code'

import type { Ask, Board, Thread, Track } from '../types'

const PANE = 'ox-board'
const TITLE = 'ox'
const EVERY_MS = 60_000
const board = atom({ plugin: 'ox-board', key: 'board' } as const, null)
const replyTo = atom({ plugin: 'ox-board', key: 'replyTo' } as const, null)

// The desktop app's PATH may not reach ~/go/bin, where make install puts ox.
const ox = (...args: string[]) => ['/bin/sh', '-c', 'exec "$(command -v ox || echo "$HOME/go/bin/ox")" "$@"', 'ox', ...args]

type ViewAsk = Omit<Ask, 'age'> & { since: string }
type View = {
  threads?: { sessionId: string; title: string; track?: string; state: string; next?: string; archivable?: boolean }[]
  asks?: { waiting?: ViewAsk[]; fixes?: ViewAsk[] }
  tracks?: Track[]
}

// Theme colors, so the panel follows light and dark.
const COLOR: Record<string, string> = {
  question: 'warning',
  review: 'warning',
  merge: 'warning',
  fix: 'error',
  working: 'suggestion',
  'in review': 'merged',
  stuck: 'error',
  parked: 'success',
  done: 'inactive',
}

// A question a plain yes answers gets a Go ahead button.
const YES_NO = /go[- ]ahead|^(want|should|shall|can|do|ready|ok|okay)\b/i

function age(since: string, now: number): string {
  const minutes = Math.max(0, Math.round((now - Date.parse(since)) / 60_000))
  if (minutes < 60) return `${minutes}m`
  if (minutes < 48 * 60) return `${Math.round(minutes / 60)}h`
  return `${Math.round(minutes / 1440)}d`
}

function count(n: number, one: string, many = `${one}s`): string {
  return `${n} ${n === 1 ? one : many}`
}

async function refresh($: EngineInterface): Promise<void> {
  const ran = await $.process
    .run(ox('now', '--brief', '--json'), { timeoutMs: 20_000 })
    .catch(err => ({ exitCode: -1, stdout: '', stderr: String(err) }))
  const now = await $.clock.now()
  const fail = (error: string) =>
    update($, board, b => (b ? { ...b, error } : { at: now, waiting: [], fixes: [], threads: [], tracks: [], archivable: 0, error }))
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
  const threads = view.threads ?? []
  const next: Board = {
    at: now,
    waiting: (view.asks?.waiting ?? []).map(ask),
    fixes: (view.asks?.fixes ?? []).map(ask),
    threads: threads.map((t): Thread => ({ id: t.sessionId, title: t.title, track: t.track, state: t.state, next: t.next })),
    tracks: view.tracks ?? [],
    archivable: threads.filter(t => t.archivable).length,
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

async function dismiss($: EngineInterface, a: Ask): Promise<void> {
  await $.process.run(ox('asks', 'drop', a.id)).catch(() => undefined)
  await refresh($)
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
    const cols = e.props.bodyColumns
    const nameCols = Math.min(18, Math.max(10, Math.floor(cols * 0.3)))
    const half = Math.max(8, Math.floor((cols - nameCols) / 2))
    const trackName = (short?: string) => b.tracks.find(t => t.short === short)?.name ?? 'No track'

    const chip = (key: string, label: string) => (
      <Text key={key} color={COLOR[key]}>
        ● {label}
      </Text>
    )
    const yours = (asks: Ask[], fixes: Ask[]) => {
      const n = (kind: string) => asks.filter(a => a.kind === kind).length
      return [
        n('question') > 0 && chip('question', count(n('question'), 'question')),
        n('review') > 0 && chip('review', count(n('review'), 'review')),
        n('merge') > 0 && chip('merge', `${n('merge')} to merge`),
        fixes.length > 0 && chip('fix', count(fixes.length, 'PR to fix', 'PRs to fix')),
      ].filter(Boolean)
    }
    const handling = (threads: Thread[]) =>
      ['working', 'stuck', 'in review', 'parked', 'done']
        .map(state => [state, threads.filter(t => t.state === state).length] as const)
        .filter(([, n]) => n > 0)
        .map(([state, n]) => chip(state, `${n} ${state}`))

    const rows = [...b.tracks.map(t => t.short), undefined]
      .map(short => ({
        short,
        asks: b.waiting.filter(a => a.track === short && a.kind !== 'memory' && a.kind !== 'recap'),
        fixes: b.fixes.filter(f => f.track === short),
        threads: b.threads.filter(t => t.track === short && t.state !== 'needs you'),
      }))
      .filter(r => r.asks.length + r.fixes.length + r.threads.length > 0)

    const table = (
      <Box flexDirection="column" marginBottom={1}>
        <Box flexDirection="row">
          <Box width={nameCols} />
          <Box width={half}>
            <Text dimColor>
              Needs you
            </Text>
          </Box>
          <Box width={half}>
            <Text dimColor>
              Threads handling it
            </Text>
          </Box>
        </Box>
        {rows.map(r => (
          <Box key={r.short ?? 'none'} flexDirection="row" marginTop={1}>
            <Box width={nameCols}>
              <Text wrap="truncate-end">{trackName(r.short)}</Text>
            </Box>
            <Box width={half} flexDirection="column">
              {yours(r.asks, r.fixes)}
            </Box>
            <Box width={half} flexDirection="column">
              {handling(r.threads)}
            </Box>
          </Box>
        ))}
      </Box>
    )

    const item = (a: Ask) => {
      const meta = [a.kind === 'question' ? trackName(a.track) : a.where, a.age].filter(Boolean).join(' · ')
      const pr = a.link?.startsWith('https://') ? <Link key={`${a.id}:pr`} href={a.link} label="Open PR" /> : null
      let title = a.what.replace(/\s*\(\/[a-z-]+\)$/, '')
      let question: string | null = null
      let buttons: RenderChildren[] = []
      switch (a.kind) {
        case 'question':
          title = a.where ?? 'A thread'
          question = a.what
          buttons = [
            ...(Input ? [<Button key={`${a.id}:reply`} label="Reply" onPress={() => void update($, replyTo, () => a.id)} />] : []),
            ...(YES_NO.test(a.what) ? [<Button key={`${a.id}:yes`} label="Go ahead" onPress={() => void sendTo($, a, 'Go ahead.')} />] : []),
          ]
          break
        case 'review':
          buttons = [<Button key={`${a.id}:start`} label="Start review" onPress={() => void askHere($, `Review ${a.link}`)} />]
          break
        case 'memory':
          buttons = [<Button key={`${a.id}:go`} label="Go through them" onPress={() => void askHere($, 'check my memory')} />]
          break
        case 'recap':
          buttons = [<Button key={`${a.id}:go`} label="Do it now" onPress={() => void askHere($, 'recap')} />]
          break
        case 'fix':
          buttons = [
            a.thread ? (
              <Button key={`${a.id}:fix`} label="Ask its thread to fix" onPress={() => void sendTo($, a, fixText(a))} />
            ) : (
              <Button key={`${a.id}:fix`} label="Fix it here" onPress={() => void askHere($, fixText(a))} />
            ),
          ]
          break
      }
      if (a.kind !== 'fix') buttons.push(<Button key={`${a.id}:drop`} label="Dismiss" onPress={() => void dismiss($, a)} />)
      return (
        <Box key={a.id} flexDirection="column" marginBottom={1}>
          <Text bold wrap="truncate-end">
            {title}
          </Text>
          {question && <Text wrap="wrap">{question}</Text>}
          <Text dimColor wrap="truncate-end">
            {meta}
          </Text>
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
          <Box flexDirection="row" flexWrap="wrap" gap={1}>
            {buttons}
            {pr}
          </Box>
        </Box>
      )
    }

    const parked = b.threads.filter(t => t.state === 'parked')
    const heading = (text: string) => (
      <Text bold>
        {text}
      </Text>
    )

    return (
      <Box flexDirection="column" width={cols}>
        <Box flexDirection="row" justifyContent="space-between" marginBottom={1}>
          <Text dimColor>
            {b.error ? `ox: ${b.error}` : 'Updates every minute'}
          </Text>
          <Button key="refresh" label="Refresh" onPress={() => void refresh($)} />
        </Box>
        {table}
        {heading(`Your turn · ${b.waiting.length}`)}
        {b.waiting.length === 0 && <Text dimColor>Nothing waits on you.</Text>}
        {b.waiting.map(item)}
        {b.fixes.length > 0 && heading(`PRs to fix · ${b.fixes.length}`)}
        {b.fixes.map(item)}
        {parked.length > 0 && heading(`Parked · ${parked.length}`)}
        {parked.map(t => (
          <Box key={t.id} flexDirection="column" marginBottom={1}>
            <Text wrap="truncate-end">{t.title}</Text>
            <Text dimColor wrap="truncate-end">
              Next: {t.next}
            </Text>
          </Box>
        ))}
        {b.archivable > 0 && (
          <Box flexDirection="row" gap={1}>
            <Text dimColor>{count(b.archivable, 'finished thread')} to archive</Text>
            <Button key="tidy" label="Tidy up" onPress={() => void askHere($, 'clean up my threads')} />
          </Box>
        )}
      </Box>
    )
  })
}
