import { expect, mock, test } from 'claude-code/testing'
import type { On } from 'claude-code'

const PANE = {
  plugin: 'ox-board',
  component: 'Pane',
  requestId: 'ox-board',
  props: {
    title: 'ox',
    isFocused: false,
    bodyColumns: 60,
    placement: 'dock',
    scroll: { offset: 0, bodyRows: 40 },
    view: {},
  },
} as const

const NOW = Date.parse('2026-10-28T12:00:00+05:30')

const VIEW = {
  at: '2026-10-28T12:00:00+05:30',
  tracks: [
    { short: 'durable', name: 'Durable agents' },
    { short: 'build', name: 'Server build split' },
    { short: 'review', name: 'Reviews' },
    { short: 'ox', name: 'ox' },
  ],
  threads: [
    { sessionId: 'local_a', title: 'Durable chat turns', track: 'durable', state: 'needs you' },
    { sessionId: 'local_e', title: 'Valkey clusters', track: 'durable', state: 'in review' },
    { sessionId: 'local_d', title: 'Left for later', track: 'durable', state: 'parked', next: 'Rebase on main' },
    { sessionId: 'local_b', title: 'Board panel', track: 'ox', state: 'needs you' },
    { sessionId: 'local_f', title: 'Server build', track: 'build', state: 'in review' },
    { sessionId: 'local_c', title: 'Old work', state: 'done' },
  ],
  asks: {
    waiting: [
      { id: 'question:local_a', kind: 'question', what: 'Is a move to GCP planned?', where: 'Durable chat turns', thread: 'local_a', track: 'durable', since: '2026-10-26T15:00:00+05:30' },
      { id: 'question:local_b', kind: 'question', what: 'Want me to merge it?', where: 'Board panel', thread: 'local_b', track: 'ox', since: '2026-10-28T11:50:00+05:30' },
      { id: 'review:backend#6786', kind: 'review', what: 'Review backend#6786 for Neeraj', link: 'https://github.com/CBX1/backend/pull/6786', track: 'review', since: '2026-10-27T13:00:00+05:30' },
      { id: 'memory', kind: 'memory', what: 'Memory: 2 suggestions (/memory-check)', since: '2026-10-28T11:00:00+05:30' },
    ],
    fixes: [
      { id: 'fix:frontend#9680', kind: 'fix', what: 'frontend#9680: CI failing (draft)', where: 'Server build', thread: 'local_f', track: 'build', link: 'https://github.com/CBX1/frontend/pull/9680', since: '2026-10-27T13:00:00+05:30' },
    ],
  },
}

// The world beneath the board: ox answers with VIEW, sends and prompts are kept.
function world(on: On, surfaces: string[], stdout = JSON.stringify(VIEW)) {
  const seen = { runs: [] as string[][], sent: [] as { to: unknown; text: string }[], prompts: [] as string[] }
  on('command.register', () => ({ value: { command: 'ox-board' } }))
  on('ui.open', () => ({ value: { isPlaced: true } }))
  on('ui.toast', () => ({ value: undefined }))
  on('session.surfaces', () => ({ value: surfaces }))
  on('process.run', (_, e) => {
    seen.runs.push([...e.argv])
    return { value: { exitCode: 0, stdout, stderr: '' } }
  })
  on('session.send', (_, e) => {
    seen.sent.push({ to: e.to, text: e.text })
    return { isDelivered: true }
  })
  on('prompt.submit', (_, e) => {
    seen.prompts.push(e.text)
    return { text: e.text, context: [] }
  })
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  return { seen, clock: mock.clock(on, { now: NOW }) }
}

const text = async (ui: { find: (q: { type: 'Text'; text: RegExp }) => Promise<{ text: string } | undefined> }, re: RegExp) =>
  (await ui.find({ type: 'Text', text: re }))?.text

const nows = (runs: string[][]) => runs.filter(argv => argv.includes('now')).length

test('the board draws a line per track and the lists on every surface', async ($, on) => {
  world(on, ['desktop'])
  await $.session.start({ cwd: '/', surface: 'desktop', isInteractive: true })

  for (const surface of ['terminal', 'desktop'] as const) {
    const ui = await $.ui.mount({ ...PANE, surface })
    expect(await text(ui, /^Durable agents$/)).toBe('Durable agents')
    expect(await text(ui, /^1 for you$/)).toBe('1 for you')
    expect(await text(ui, /in review · 1 parked$/)).toBe('1 for you · 1 in review · 1 parked')
    expect(await text(ui, /^For you$/)).toBe('For you')
    expect(await text(ui, /GCP/)).toBe('Is a move to GCP planned?')
    expect(await text(ui, /^Durable chat turns · /)).toBe('Durable chat turns · 45h')
    expect(await text(ui, /^Memory/)).toBe('Memory: 2 suggestions')
    expect(await text(ui, /^PRs to fix$/)).toBe('PRs to fix')
    expect(await text(ui, /^Next:/)).toBe('Next: Rebase on main')
    await ui.unmount()
  }
})

test('each item has at most one button, and it does the one thing', async ($, on) => {
  const { seen } = world(on, ['desktop'])
  await $.session.start({ cwd: '/', surface: 'desktop', isInteractive: true })
  const ui = await $.ui.mount({ ...PANE, surface: 'desktop' })

  for (const key of ['question:local_b:yes', 'review:backend#6786:drop', 'refresh']) {
    expect(await ui.find({ type: 'Button', key })).toBeUndefined()
  }

  await ui.press({ key: 'question:local_b:reply' })
  await ui.input({ key: 'question:local_b:answer', text: 'go ahead' })
  expect(seen.sent.at(-1)).toEqual({ to: 'local_b', text: 'go ahead' })

  await ui.press({ key: 'fix:frontend#9680:fix' })
  expect(seen.sent.at(-1)?.to).toBe('local_f')
  expect(seen.sent.at(-1)?.text).toMatch(/^Fix frontend#9680: CI failing: https:\/\/github.com\/CBX1\/frontend\/pull\/9680\. /)

  await ui.press({ key: 'review:backend#6786:start' })
  expect(seen.prompts.at(-1)).toBe('Review https://github.com/CBX1/backend/pull/6786')

  await ui.press({ key: 'memory:start' })
  expect(seen.prompts.at(-1)).toBe('check my memory')
  await ui.unmount()
})

test('a session with no pane never runs ox until the board is asked for', async ($, on) => {
  const { seen, clock } = world(on, [])
  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
  expect(seen.runs.length).toBe(0)
  await $.command.run({ command: 'ox-board' })
  expect(nows(seen.runs)).toBe(1)
  expect(seen.runs.some(argv => argv.slice(-3).join(' ') === 'asks refresh --if-due')).toBe(true)
  await clock.advance(15_000)
  expect(nows(seen.runs)).toBe(2)
})

test('the board says when ox prints something that is not JSON', async ($, on) => {
  world(on, [], 'Update available\n{')
  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
  await $.command.run({ command: 'ox-board' })

  const ui = await $.ui.mount({ ...PANE, surface: 'desktop' })
  expect(await text(ui, /^ox:/)).toBe("ox: ox printed something that isn't JSON")
  await ui.unmount()
})
