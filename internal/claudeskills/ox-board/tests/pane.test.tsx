import { expect, test } from 'claude-code/testing'

const PANE = {
  plugin: 'ox-board',
  component: 'Pane',
  requestId: 'ox-board',
  props: {
    title: 'ox',
    isFocused: false,
    bodyColumns: 40,
    placement: 'dock',
    scroll: { offset: 0, bodyRows: 30 },
    view: {},
  },
} as const

const NOW = Date.parse('2026-10-28T12:00:00+05:30')

const VIEW = {
  at: '2026-10-28T12:00:00+05:30',
  threads: [
    { sessionId: 'local_a', title: 'Durable chat turns', track: 'durable', state: 'needs you', ask: 'Is a move to GCP planned?' },
    { sessionId: 'local_b', title: 'Board panel', track: 'ox', state: 'working' },
    { sessionId: 'local_c', title: 'Old work', state: 'done', archivable: true },
  ],
  asks: {
    waiting: [
      { id: 'question:local_a', what: 'Is a move to GCP planned?', where: 'Durable chat turns', link: 'local_a', since: '2026-10-26T16:00:00+05:30' },
      { id: 'review:backend#6786', what: 'Review backend#6786 for Neeraj', link: 'https://github.com/CBX1/backend/pull/6786', since: '2026-10-28T11:30:00+05:30' },
    ],
    fixes: [{ id: 'fix:frontend#9680', what: 'frontend#9680: CI failing (draft)', where: 'Server build', since: '2026-10-28T09:00:00+05:30' }],
  },
}

test('the board draws ox now on every surface', async ($, on) => {
  on('command.register', () => ({ value: { command: 'ox-board' } }))
  on('ui.open', () => ({ value: { isPlaced: true } }))
  on('session.surfaces', () => ({ value: ['desktop'] }))
  on('clock.every', () => ({ value: undefined }))
  on('clock.now', () => ({ value: NOW }))
  on('process.run', () => ({ value: { exitCode: 0, stdout: JSON.stringify(VIEW), stderr: '' } }))
  on('session.start', (_, e) => ({ cwd: e.cwd }))

  await $.session.start({ cwd: '/', surface: 'desktop', isInteractive: true })
  await $.command.run({ command: 'ox-board' })

  for (const surface of ['terminal', 'desktop'] as const) {
    const ui = await $.ui.mount({ ...PANE, surface })
    expect((await ui.find({ type: 'Text', text: /^Waiting on you/ }))?.text).toBe('Waiting on you (2)')
    expect((await ui.find({ type: 'Text', text: /Durable chat turns ·/ }))?.text).toBe('Durable chat turns · 44h')
    expect((await ui.find({ type: 'Text', text: /^Threads can fix/ }))?.text).toBe('Threads can fix (1)')
    expect((await ui.find({ type: 'Text', text: /^working/ }))?.text).toBe('working 1 · stuck 0 · in review 0 · done 1')
    expect((await ui.find({ type: 'Text', text: /archived/ }))?.text).toBe('1 done thread can be archived')
    await ui.unmount()
  }
})

test('the board says when ox fails', async ($, on) => {
  on('command.register', () => ({ value: { command: 'ox-board' } }))
  on('ui.open', () => ({ value: { isPlaced: true } }))
  on('session.surfaces', () => ({ value: ['desktop'] }))
  on('clock.every', () => ({ value: undefined }))
  on('clock.now', () => ({ value: NOW }))
  on('process.run', () => ({ value: { exitCode: 1, stdout: '', stderr: 'Error: config not found\nusage' } }))
  on('session.start', (_, e) => ({ cwd: e.cwd }))

  await $.session.start({ cwd: '/', surface: 'desktop', isInteractive: true })
  await $.command.run({ command: 'ox-board' })

  const ui = await $.ui.mount({ ...PANE, surface: 'desktop' })
  expect((await ui.find({ type: 'Text', text: /^ox:/ }))?.text).toBe('ox: Error: config not found')
  await ui.unmount()
})
