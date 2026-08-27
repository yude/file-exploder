package cmd

import (
	"encoding/json"
	"os"

	"github.com/spf13/cobra"
	"github.com/yude/file-exploder/server/internal/config"
	"github.com/yude/file-exploder/server/internal/queue"
)

var cancelCmd = &cobra.Command{
	Use:   "cancel [job-id]",
	Short: "Cancel a pending job",
	Args:  cobra.ExactArgs(1),
	RunE:  runCancel,
}

func init() {
	rootCmd.AddCommand(cancelCmd)
}

func runCancel(cmd *cobra.Command, args []string) error {
	cfg := config.DefaultConfig()
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}

	q, err := queue.NewSQLiteQueue(cfg.DBPath)
	if err != nil {
		return err
	}
	defer q.Close()

	if err := q.CancelJob(args[0]); err != nil {
		return err
	}

	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(map[string]string{
		"id":     args[0],
		"status": string(queue.StatusCancelled),
	})
}
