export type Ask = {
  id: string
  kind: string
  what: string
  where?: string // the thread's title
  thread?: string // the thread's id
  track?: string
  link?: string
  age: string
  minutes?: number
}

export type Thread = { id: string; title: string; track?: string; state: string; next?: string }

export type Track = { short: string; name: string }

export type Board = {
  at: number
  waiting: Ask[]
  fixes: Ask[]
  threads: Thread[]
  tracks: Track[]
  archivable: number
  error?: string
}

declare module 'claude-code' {
  interface PluginState {
    'ox-board': { board: Board | null; replyTo: string | null }
  }
}
