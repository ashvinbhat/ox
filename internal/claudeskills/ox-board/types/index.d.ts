// A thread stopped on Ashvin: the stop's kind when the thread named one, its
// title, what it waits on, since when.
export type Need = { id: string; kind?: string; title: string; what: string; since: string; thread: string }

// A thread with work moving, under its ticket when it has one.
export type Work = { ticket?: string; title: string; state: string; prs?: string[]; since: string; thread: string }

// problems are ox's own background jobs that keep failing; error is this
// panel's last failed call to ox.
export type Board = {
  at: number
  needs: Need[]
  snoozed: number
  inProgress: Work[]
  landed: string[]
  problems?: string[]
  error?: string
}

declare module 'claude-code' {
  interface PluginState {
    'ox-board': { board: Board | null }
  }
}
