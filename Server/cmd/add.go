package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/yude/file-exploder/server/internal/config"
	"github.com/yude/file-exploder/server/internal/executor"
	"github.com/yude/file-exploder/server/internal/queue"
)

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a file operation to the queue",
	Args:  cobra.NoArgs,
	RunE:  runAdd,
}

var (
	addType string
	addSrc  string
	addDst  string
	addMode string
)

func init() {
	addCmd.Flags().StringVar(&addType, "type", "", "Operation type: rename, move, delete, copy, mkdir, chmod")
	addCmd.Flags().StringVar(&addSrc, "src", "", "Source path")
	addCmd.Flags().StringVar(&addDst, "dst", "", "Destination path")
	addCmd.Flags().StringVar(&addMode, "mode", "", "File mode (for chmod)")
	if err := addCmd.MarkFlagRequired("type"); err != nil {
		panic(err)
	}
	rootCmd.AddCommand(addCmd)
}

func runAdd(cmd *cobra.Command, args []string) error {
	// Validate job type
	validTypes := map[string]bool{
		"rename": true, "move": true, "delete": true,
		"copy": true, "mkdir": true, "chmod": true,
	}
	if !validTypes[addType] {
		return fmt.Errorf("invalid operation type: %s", addType)
	}

	// Basic validation
	if (addType == "rename" || addType == "move" || addType == "copy") && (addSrc == "" || addDst == "") {
		return fmt.Errorf("both --src and --dst are required for %s", addType)
	}
	if addType == "delete" && addSrc == "" {
		return fmt.Errorf("--src is required for delete")
	}
	if addType == "mkdir" && addDst == "" {
		return fmt.Errorf("--dst is required for mkdir")
	}
	if addType == "chmod" {
		if addDst == "" || addMode == "" {
			return fmt.Errorf("both --dst and --mode are required for chmod")
		}
		// Reject a bad mode here so it never reaches the queue, using the same
		// parser the executor will apply.
		if _, err := executor.ParseFileMode(addMode); err != nil {
			return err
		}
	}
	switch addType {
	case "rename", "move", "copy":
		if addMode != "" {
			return fmt.Errorf("--mode is not valid for %s", addType)
		}
	case "delete":
		if addDst != "" || addMode != "" {
			return fmt.Errorf("--dst and --mode are not valid for delete")
		}
	case "mkdir":
		if addSrc != "" || addMode != "" {
			return fmt.Errorf("--src and --mode are not valid for mkdir")
		}
	case "chmod":
		if addSrc != "" {
			return fmt.Errorf("--src is not valid for chmod")
		}
	}
	for _, path := range []string{addSrc, addDst} {
		if path == "" {
			continue
		}
		if !filepath.IsAbs(path) {
			return fmt.Errorf("paths must be absolute: %s", path)
		}
		if strings.ContainsRune(path, '\x00') {
			return fmt.Errorf("paths cannot contain NUL bytes")
		}
		if filepath.Clean(path) == "/" {
			return fmt.Errorf("refusing to queue an operation on the filesystem root")
		}
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
