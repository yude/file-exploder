package cmd

import (
	"encoding/json"
	"fmt"
	"os"

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
	Args:  cobra.NoArgs,
	RunE:  runLog,
}

func init() {
	logCmd.Flags().IntVarP(&logLimit, "limit", "l", 20, "Number of recent jobs to show")
	rootCmd.AddCommand(logCmd)
}

func runLog(cmd *cobra.Command, args []string) error {
	if logLimit <= 0 {
		return fmt.Errorf("limit must be greater than 0")
	}
	if logLimit > 1000 {
		return fmt.Errorf("limit must not exceed 1000")
	}

	cfg := config.DefaultConfig()
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}

	q, err := queue.NewSQLiteQueue(cfg.DBPath)
	if err != nil {
		return err
	}
	defer q.Close()

	jobs, err := q.GetRecentLogs(logLimit)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(jobs)
}
