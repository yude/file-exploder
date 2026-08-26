package cmd

import (
	"github.com/spf13/cobra"
	"github.com/yude/file-exploder/server/internal/config"
	"github.com/yude/file-exploder/server/internal/queue"
)

var (
	logLimit int
)

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Show recent job logs",
	RunE:  runLog,
}

func init() {
	logCmd.Flags().IntVarP(&logLimit, "limit", "l", 20, "Number of recent jobs to show")
	rootCmd.AddCommand(logCmd)
}

func runLog(cmd *cobra.Command, args []string) error {
	cfg := config.DefaultConfig()
	q, err := queue.NewSQLiteQueue(cfg.DBPath)
	if err != nil {
		return err
	}
	defer q.Close()

	jobs, err := q.GetRecentLogs(logLimit)
	if err != nil {
		return err
	}

	return printJSON(jobs)
}
