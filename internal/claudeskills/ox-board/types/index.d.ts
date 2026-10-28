// One thread that needs Ashvin: its title, what it's waiting on, and for how long.
export type Item = { id: string; title: string; waiting: string; age: string }

export type Board = { at: number; items: Item[]; error?: string }

declare module 'claude-code' {
  interface PluginState {
    'ox-board': { board: Board | null }
  }
}
