package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	addCmd.MarkFlagRequired("type")
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
		mode, err := strconv.ParseUint(addMode, 8, 12)
		if err != nil || len(addMode) == 0 || len(addMode) > 4 || mode > 0777 {
			return fmt.Errorf("invalid mode format: %s", addMode)
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
