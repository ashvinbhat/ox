import { expect, mock, test } from 'claude-code/testing'
import type { On } from 'claude-code'

const PANE = {
  plugin: 'ox-board',
  component: 'Pane',
  requestId: 'ox-board',
  props: {
    title: '🐂 ox',
    isFocused: false,
    bodyColumns: 60,
    placement: 'dock',
    scroll: { offset: 0, bodyRows: 40 },
    view: {},
  },
} as const

const NOW = Date.parse('2026-10-28T12:00:00+05:30')

const VIEW = {
  threads: [
    { sessionId: 'local_a', title: 'Durable chat turns', state: 'needs you', lastActive: '2026-10-26T15:00:00+05:30' },
    { sessionId: 'local_s', title: 'Valkey clusters', state: 'stuck', lastActive: '2026-10-28T10:00:00+05:30' },
    { sessionId: 'local_r', title: 'In review only', state: 'in review', lastActive: '2026-10-28T11:00:00+05:30' },
    { sessionId: 'local_x', title: 'Cleared earlier', state: 'stuck', lastActive: '2026-10-28T09:00:00+05:30', dropped: true },
  ],
  asks: {
    waiting: [
      { id: 'question:local_a', kind: 'question', what: 'Is a move to GCP planned?', where: 'Durable chat turns', thread: 'local_a', since: '2026-10-26T15:00:00+05:30', settled: 'backend#6900 merged since' },
      { id: 'review:backend#6786', kind: 'review', what: 'Review backend#6786 for Neeraj', since: '2026-10-27T13:00:00+05:30' },
      { id: 'merge:ox#24', kind: 'merge', what: 'Merge ox#24: feat: ox board', where: 'Board panel', thread: 'local_b', since: '2026-10-28T11:30:00+05:30' },
      { id: 'memory', kind: 'memory', what: 'Memory: 2 suggestions (/memory-check)', since: '2026-10-28T11:00:00+05:30' },
    ],
    fixes: [{ id: 'fix:frontend#9680', kind: 'fix', what: 'frontend#9680: CI failing', where: 'Server build', thread: 'local_f', since: '2026-10-27T13:00:00+05:30' }],
  },
}

function world(on: On, surfaces: string[], stdout = JSON.stringify(VIEW)) {
  const runs: string[][] = []
  on('command.register', () => ({ value: { command: 'ox-board' } }))
  on('ui.open', () => ({ value: { isPlaced: true } }))
  on('session.surfaces', () => ({ value: surfaces }))
  on('process.run', (_, e) => {
    runs.push([...e.argv])
    return { value: { exitCode: 0, stdout, stderr: '' } }
  })
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  return { runs, clock: mock.clock(on, { now: NOW }) }
}

const nows = (runs: string[][]) => runs.filter(argv => argv.includes('now')).length

test('the panel lists only threads that need you, with what each waits on', async ($, on) => {
  world(on, ['desktop'])
  await $.session.start({ cwd: '/', surface: 'desktop', isInteractive: true })

  for (const surface of ['terminal', 'desktop'] as const) {
    const ui = await $.ui.mount({ ...PANE, surface })
    expect((await ui.findAll({ type: 'Markdown' })).map(m => m.props.text)).toEqual([
      '**Durable chat turns**  \nIs a move to GCP planned? (backend\\#6900 merged since; still needed?) · 45h',
      '**Board panel**  \nox\\#24 is ready to merge · 30m',
      '**Valkey clusters**  \nWent quiet mid-turn; it may have stopped · 2h',
    ])
    expect((await ui.findAll({ type: 'Button' })).map(b => b.key)).toEqual([
      'question:local_a:clear',
      'merge:ox#24:clear',
      'stuck:local_s:clear',
      'clear',
    ])
    await ui.unmount()
  }
})

test('× drops only its own item, then rereads ox', async ($, on) => {
  const { runs } = world(on, [])
  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
  await $.command.run({ command: 'ox-board' })
  const ui = await $.ui.mount({ ...PANE, surface: 'desktop' })
  await ui.press({ key: 'merge:ox#24:clear' })
  const drop = runs.find(argv => argv.includes('drop'))
  expect(drop?.slice(drop.indexOf('asks'))).toEqual(['asks', 'drop', 'merge:ox#24'])
  expect(nows(runs)).toBe(2)
  await ui.unmount()
})

test('Clear all drops every item shown, then rereads ox', async ($, on) => {
  const { runs } = world(on, [])
  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
  await $.command.run({ command: 'ox-board' })
  const ui = await $.ui.mount({ ...PANE, surface: 'desktop' })
  await ui.press({ key: 'clear' })
  const drop = runs.find(argv => argv.includes('drop'))
  expect(drop?.slice(drop.indexOf('asks'))).toEqual(['asks', 'drop', 'question:local_a', 'merge:ox#24', 'stuck:local_s'])
  expect(nows(runs)).toBe(2)
  await ui.unmount()
})

test('a session with no pane never runs ox until the board is asked for', async ($, on) => {
  const { runs, clock } = world(on, [])
  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
  expect(runs.length).toBe(0)
  await $.command.run({ command: 'ox-board' })
  expect(nows(runs)).toBe(1)
  expect(runs.some(argv => argv.slice(-3).join(' ') === 'asks refresh --if-due')).toBe(true)
  await clock.advance(15_000)
  expect(nows(runs)).toBe(2)
})

test('the panel says when nothing needs you, and when ox fails', async ($, on) => {
  world(on, [], 'Update available\n{')
  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
  await $.command.run({ command: 'ox-board' })
  const ui = await $.ui.mount({ ...PANE, surface: 'desktop' })
  expect((await ui.find({ type: 'Markdown' }))?.props.text).toBe("Nothing needs you right now.\n\n_ox: ox printed something that isn't JSON_")
  expect(await ui.findAll({ type: 'Button' })).toEqual([])
  await ui.unmount()
})

test('a job that keeps failing shows under the items until it succeeds', async ($, on) => {
  const problem = 'PR state is 2h old: gh: authentication failed'
  let view: object = { ...VIEW, problems: [problem] }
  on('command.register', () => ({ value: { command: 'ox-board' } }))
  on('ui.open', () => ({ value: { isPlaced: true } }))
  on('session.surfaces', () => ({ value: [] }))
  on('process.run', () => ({ value: { exitCode: 0, stdout: JSON.stringify(view), stderr: '' } }))
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  const clock = mock.clock(on, { now: NOW })
  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
  await $.command.run({ command: 'ox-board' })
  const ui = await $.ui.mount({ ...PANE, surface: 'desktop' })
  const texts = async () => (await ui.findAll({ type: 'Markdown' })).map(m => String(m.props.text))
  expect((await texts()).at(-1)).toBe(`_ox: ${problem}_`)

  view = VIEW
  await clock.advance(15_000)
  expect((await texts()).some(t => t.startsWith('_ox:'))).toBe(false)
  await ui.unmount()
})

test('a failed clear keeps the items and says why', async ($, on) => {
  on('command.register', () => ({ value: { command: 'ox-board' } }))
  on('ui.open', () => ({ value: { isPlaced: true } }))
  on('session.surfaces', () => ({ value: [] }))
  on('process.run', (_, e) =>
    e.argv.includes('drop')
      ? { value: { exitCode: 1, stdout: '', stderr: 'Error: locked\n' } }
      : { value: { exitCode: 0, stdout: JSON.stringify(VIEW), stderr: '' } },
  )
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  mock.clock(on, { now: NOW })
  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
  await $.command.run({ command: 'ox-board' })
  const ui = await $.ui.mount({ ...PANE, surface: 'desktop' })
  await ui.press({ key: 'clear' })
  const texts = (await ui.findAll({ type: 'Markdown' })).map(m => m.props.text)
  expect(texts.length).toBe(4)
  expect(texts[3]).toBe('_ox: Error: locked_')
  await ui.unmount()
})

test('findings waiting for a go show one line per ticket, below the threads', async ($, on) => {
  const view = {
    ...VIEW,
    findings: [
      { id: 'findings:CB-18304', title: 'CB-18304 Video transcription', count: 3, since: '2026-10-28T11:00:00+05:30' },
      { id: 'findings:track:ox', title: 'ox (no ticket)', count: 1, since: '2026-10-28T11:50:00+05:30' },
      { id: 'findings:CB-1', title: 'CB-1 Cleared', count: 2, since: '2026-10-27T11:00:00+05:30', dropped: true },
    ],
  }
  world(on, ['desktop'], JSON.stringify(view))
  await $.session.start({ cwd: '/', surface: 'desktop', isInteractive: true })
  const ui = await $.ui.mount({ ...PANE, surface: 'desktop' })
  const texts = (await ui.findAll({ type: 'Markdown' })).map(m => m.props.text)
  expect(texts.slice(3)).toEqual([
    '**CB-18304 Video transcription**  \n3 findings waiting for your go · 1h',
    '**ox (no ticket)**  \n1 finding waiting for your go · 10m',
  ])
  await ui.unmount()
})
