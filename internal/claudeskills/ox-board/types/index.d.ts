export type Ask = { id: string; what: string; where?: string; link?: string; age: string }

export type Thread = { id: string; title: string; track?: string; state: string; ask?: string }

export type Board = {
  at: number
  waiting: Ask[]
  fixes: Ask[]
  threads: Thread[]
  archivable: number
  error?: string
}

declare module 'claude-code' {
  interface PluginState {
    'ox-board': { board: Board | null }
  }
}
