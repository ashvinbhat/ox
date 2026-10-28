package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/desktop"
	"github.com/ashvinbhat/ox/internal/park"
)

var (
	parkNext    string
	parkSession string
)

var parkCmd = &cobra.Command{
	Use:   "park",
	Short: "Mark this thread parked, with the first step for when you come back",
	Long: `Records that Ashvin left this desktop thread on purpose, with the next
step. Until the thread is active again, ox now shows it as parked with that
step instead of done, it is never archivable, and its question is not an ask.

The /park skill runs this after writing the thread's resumption note.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()
		id := parkSession
		if id == "" {
			id = os.Getenv("CLAUDE_CODE_HOST_SESSION_ID")
		}
		if id == "" {
			return fmt.Errorf("no thread to park: run inside a desktop thread or pass --session")
		}
		r := park.Record{SessionID: id, Next: parkNext, At: time.Now()}
		if s, err := desktop.Get(desktop.DefaultDir(), id); err == nil {
			r.Title = s.Title
		}
		if err := park.Save(cfg.Home, r); err != nil {
			return err
		}
		fmt.Printf("Parked %q. Next: %s\n", r.Title, r.Next)
		return nil
	},
}

func init() {
	parkCmd.Flags().StringVar(&parkNext, "next", "", "the first step when you come back (required)")
	parkCmd.Flags().StringVar(&parkSession, "session", "", "thread id (default: this thread)")
	_ = parkCmd.MarkFlagRequired("next")
	rootCmd.AddCommand(parkCmd)
}
