package cmd

import (
	"encoding/json"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/yude/file-exploder/server/internal/config"
	"github.com/yude/file-exploder/server/internal/queue"
)

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a file operation to the queue",
	RunE:  runAdd,
}

var (
	addType  string
	addSrc   string
	addDst   string
	addMode  string
)

func init() {
	addCmd.Flags().StringVar(&addType, "type", "", "Operation type: rename, move, delete, copy, mkdir, chmod, upload, download")
	addCmd.Flags().StringVar(&addSrc, "src", "", "Source path")
	addCmd.Flags().StringVar(&addDst, "dst", "", "Destination path")
	addCmd.Flags().StringVar(&addMode, "mode", "", "File mode (for chmod)")
	addCmd.MarkFlagRequired("type")
	rootCmd.AddCommand(addCmd)
}

func runAdd(cmd *cobra.Command, args []string) error {
	cfg := config.DefaultConfig()
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}

	q, err := queue.NewSQLiteQueue(cfg.DBPath)
	if err != nil {
		return err
	}
	defer q.Close()

	job := &queue.Job{
		ID:        uuid.New().String(),
		Type:      queue.JobType(addType),
		SrcPath:   addSrc,
		DstPath:   addDst,
		Mode:      addMode,
		Status:    queue.StatusPending,
		CreatedAt: time.Now(),
	}

	if err := q.AddJob(job); err != nil {
		return err
	}

	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(map[string]string{
		"id":     job.ID,
		"status": "pending",
	})
}
