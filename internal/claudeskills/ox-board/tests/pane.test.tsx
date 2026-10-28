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
  threads: [
    { sessionId: 'local_a', title: 'Durable chat turns', state: 'needs you', lastActive: '2026-10-26T15:00:00+05:30' },
    { sessionId: 'local_s', title: 'Valkey clusters', state: 'stuck', lastActive: '2026-10-28T10:00:00+05:30' },
    { sessionId: 'local_r', title: 'In review only', state: 'in review', lastActive: '2026-10-28T11:00:00+05:30' },
  ],
  asks: {
    waiting: [
      { id: 'question:local_a', kind: 'question', what: 'Is a move to GCP planned?', where: 'Durable chat turns', thread: 'local_a', since: '2026-10-26T15:00:00+05:30' },
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
    expect((await ui.find({ type: 'Markdown' }))?.props.text).toBe(
      [
        '**Durable chat turns**  \nIs a move to GCP planned? · 45h',
        '**Board panel**  \nox\\#24 is ready to merge · 30m',
        '**Valkey clusters**  \nWent quiet mid-turn; it may have stopped · 2h',
      ].join('\n\n'),
    )
    expect(await ui.findAll({ type: 'Button' })).toEqual([])
    await ui.unmount()
  }
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
  await ui.unmount()
})
