// One thread that needs Ashvin: its title, what it's waiting on, and for how long.
export type Item = { id: string; title: string; waiting: string; age: string }

// problems are ox's own background jobs that keep failing; error is this
// panel's last failed call to ox.
export type Board = { at: number; items: Item[]; problems?: string[]; error?: string }

declare module 'claude-code' {
  interface PluginState {
    'ox-board': { board: Board | null }
  }
}
