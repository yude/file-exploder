package cmd

import (
	"github.com/spf13/cobra"
	"github.com/yude/file-exploder/server/internal/config"
	"github.com/yude/file-exploder/server/internal/queue"
)

var listCmd = &cobra.Command{
	Use:   "list [path]",
	Short: "List directory contents (for UI integration)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	cfg := config.DefaultConfig()
	q, err := queue.NewSQLiteQueue(cfg.DBPath)
	if err != nil {
		return err
	}
	defer q.Close()

	_ = q

	// This is a placeholder - the actual directory listing
	// is done via SFTP on the client side.
	// This command exists for future server-side listing needs.
	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	return printJSON(map[string]string{
		"path":    path,
		"note":    "Directory listing is handled via SFTP on the client side",
		"version": "1.0.0",
	})
}
