package cli

import (
	"fmt"

	"github.com/ashvinbhat/ox/internal/version"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the ox build version",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(version.Version)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
