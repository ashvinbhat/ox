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

const NOW = Date.parse('2026-11-11T15:00:00+05:30')
const A = 'local_aaaa-1111'
const href = (thread: string) => `https://ox.invalid/thread/${thread}`

const VIEW = {
  board: {
    needs: [
      { id: `question:${A}`, kind: 'Send', title: 'Landing migration', what: 'post the reply to Neeraj?', since: '2026-11-11T13:00:00+05:30', thread: A },
      { id: 'merge:ox#45', kind: 'Merge', title: 'ox stops', what: 'ox#45 is ready to merge', since: '2026-11-11T14:30:00+05:30', thread: 'local_bbbb' },
      { id: 'question:local_cccc', title: 'Old thread', what: 'say whether to go on', since: '2026-11-08T15:00:00+05:30', thread: 'local_cccc' },
    ],
    snoozed: 2,
    inFlight: [
      { title: 'Background tasks', state: 'working', since: '2026-11-11T14:00:00+05:30', thread: 'local_e1' },
      { ticket: 'CB-20605 Land user in onboarding', title: 'CB-20605 BE', state: 'stuck', since: '2026-11-11T12:00:00+05:30', thread: 'local_f2' },
      { ticket: 'CB-20605 Land user in onboarding', title: 'CB-20605 FE', state: 'in review', prs: ['frontend#10019'], since: '2026-11-11T13:00:00+05:30', thread: 'local_d3' },
    ],
    landed: ['ox#43', 'ox#44'],
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

test('the panel shows the stops, then the work in flight by ticket, then what landed', async ($, on) => {
  world(on, ['desktop'])
  await $.session.start({ cwd: '/', surface: 'desktop', isInteractive: true })

  for (const surface of ['terminal', 'desktop'] as const) {
    const ui = await $.ui.mount({ ...PANE, surface })
    expect((await ui.findAll({ type: 'Markdown' })).map(m => m.props.text)).toEqual([
      `[**Send · Landing migration**](${href(A)})  \n[post the reply to Neeraj? · 2h](${href(A)})`,
      `[**Merge · ox stops**](${href('local_bbbb')})  \n[ox\\#45 is ready to merge · 30m](${href('local_bbbb')})`,
      `[**Old thread**](${href('local_cccc')})  \n[say whether to go on · 3d](${href('local_cccc')})`,
      `[**Background tasks**](${href('local_e1')})  \n[working · 1h](${href('local_e1')})`,
      '**CB-20605 Land user in onboarding**',
      `[CB-20605 BE](${href('local_f2')})  \n[went quiet mid-turn · 3h](${href('local_f2')})`,
      `[CB-20605 FE](${href('local_d3')})  \n[frontend\\#10019 · in review · 2h](${href('local_d3')})`,
    ])
    expect((await ui.findAll({ type: 'Text' })).map(t => t.text)).toEqual([
      'NEEDS YOU · 3',
      '2 snoozed',
      'IN FLIGHT · 3',
      'Landed today: ox#43, ox#44',
    ])
    expect((await ui.findAll({ type: 'Button' })).map(b => b.key)).toEqual([
      `question:${A}:snooze`,
      'merge:ox#45:snooze',
      'question:local_cccc:snooze',
      'snooze-all',
    ])
    await ui.unmount()
  }
})

test('a click on an item opens its thread in the Claude app', async ($, on) => {
  const { runs } = world(on, [])
  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
  await $.command.run({ command: 'ox-board' })
  const ui = await $.ui.mount({ ...PANE, surface: 'desktop' })
  await ui.press({ key: `need:question:${A}`, link: { href: href(A) } })
  await ui.press({ key: 'flight:local_d3', link: { href: href('local_d3') } })
  expect(runs.filter(argv => argv[0] === '/usr/bin/open')).toEqual([
    ['/usr/bin/open', `claude://claude.ai/epitaxy/${A}`],
    ['/usr/bin/open', 'claude://claude.ai/epitaxy/local_d3'],
  ])
  await ui.unmount()
})

test('× snoozes only its own item, then rereads ox', async ($, on) => {
  const { runs } = world(on, [])
  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
  await $.command.run({ command: 'ox-board' })
  const ui = await $.ui.mount({ ...PANE, surface: 'desktop' })
  await ui.press({ key: 'merge:ox#45:snooze' })
  const drop = runs.find(argv => argv.includes('drop'))
  expect(drop?.slice(drop.indexOf('asks'))).toEqual(['asks', 'drop', 'merge:ox#45'])
  expect(nows(runs)).toBe(2)
  await ui.unmount()
})

test('Snooze all snoozes every stop shown, and nothing in flight', async ($, on) => {
  const { runs } = world(on, [])
  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
  await $.command.run({ command: 'ox-board' })
  const ui = await $.ui.mount({ ...PANE, surface: 'desktop' })
  await ui.press({ key: 'snooze-all' })
  const drop = runs.find(argv => argv.includes('drop'))
  expect(drop?.slice(drop.indexOf('asks'))).toEqual(['asks', 'drop', `question:${A}`, 'merge:ox#45', 'question:local_cccc'])
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
  expect((await ui.findAll({ type: 'Markdown' })).map(m => m.props.text)).toEqual([
    'Nothing needs you right now.',
    "_ox: ox printed something that isn't JSON_",
  ])
  expect(await ui.findAll({ type: 'Button' })).toEqual([])
  await ui.unmount()
})

test('a job that keeps failing shows at the bottom until it succeeds', async ($, on) => {
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

test('a failed snooze keeps the items and says why', async ($, on) => {
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
  await ui.press({ key: 'snooze-all' })
  const texts = (await ui.findAll({ type: 'Markdown' })).map(m => m.props.text)
  expect(texts.length).toBe(8)
  expect(texts[7]).toBe('_ox: Error: locked_')
  await ui.unmount()
})
