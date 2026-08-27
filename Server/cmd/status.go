package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/yude/file-exploder/server/internal/config"
	"github.com/yude/file-exploder/server/internal/queue"
)

var statusCmd = &cobra.Command{
	Use:   "status [job-id]",
	Short: "Show queue status or a specific job",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfg := config.DefaultConfig()
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}

	q, err := queue.NewSQLiteQueue(cfg.DBPath)
	if err != nil {
		return err
	}
	defer q.Close()

	if len(args) == 1 {
		job, err := q.GetJob(args[0])
		if err != nil {
			return fmt.Errorf("job not found: %s", args[0])
		}
		enc := json.NewEncoder(os.Stdout)
		return enc.Encode(job)
	}

	jobs, err := q.GetActiveJobs()
	if err != nil {
		return err
	}

	output := map[string]interface{}{
		"total": len(jobs),
		"jobs":  jobs,
	}
	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(output)
}
