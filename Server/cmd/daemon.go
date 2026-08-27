package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/yude/file-exploder/server/internal/config"
	"github.com/yude/file-exploder/server/internal/executor"
	"github.com/yude/file-exploder/server/internal/queue"
	"golang.org/x/sys/unix"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start the file-exploder daemon",
	RunE:  runDaemon,
}

func init() {
	rootCmd.AddCommand(daemonCmd)
}

func runDaemon(cmd *cobra.Command, args []string) error {
	cfg := config.DefaultConfig()
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}

	lockFile, err := os.OpenFile(cfg.LockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("failed to open daemon lock: %w", err)
	}
	defer lockFile.Close()
	if err := unix.Flock(int(lockFile.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fmt.Errorf("another file-exploder daemon is already running: %w", err)
	}
	defer unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)

	rotateLogIfLarge(cfg.LogPath)
	f, err := os.OpenFile(cfg.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer f.Close()

	logger := log.New(f, "[file-exploder] ", log.LstdFlags|log.Lshortfile)
	logger.Println("Daemon starting...")

	q, err := queue.NewSQLiteQueue(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("failed to open queue: %w", err)
	}
	defer q.Close()

	if err := q.ResetRunningJobs(); err != nil {
		logger.Printf("Failed to reset running jobs: %v", err)
	}

	exec := executor.NewExecutor(q)

	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	logger.Println("Daemon started, processing queue every 1s")

	for {
		select {
		case <-ctx.Done():
			logger.Println("Shutdown signal received, stopping")
			return nil
		case <-ticker.C:
			processPendingJobs(ctx, q, exec, logger)
		}
	}
}

// maxLogBytes bounds queue.log. The daemon appends a few lines per job and
// never truncates, so a long-lived install would otherwise grow one file
// forever.
const maxLogBytes = 8 << 20

// rotateLogIfLarge moves an oversized log aside at startup, keeping one
// previous generation. Failures are deliberately ignored: losing log history is
// not a reason to refuse to start.
func rotateLogIfLarge(path string) {
	info, err := os.Stat(path)
	if err != nil || info.Size() < maxLogBytes {
		return
	}
	_ = os.Rename(path, path+".1")
}

func processPendingJobs(ctx context.Context, q queue.Queue, exec *executor.Executor, logger *log.Logger) {
	jobs, err := q.GetPendingJobs()
	if err != nil {
		logger.Printf("Error fetching pending jobs: %v", err)
		return
	}

	for _, job := range jobs {
		// Stop claiming work as soon as a shutdown is requested. The job
		// already in flight still runs to completion; the rest stay pending
		// for the next start instead of being reset to failed.
		if ctx.Err() != nil {
			logger.Printf("Shutdown requested, leaving job %s pending", job.ID)
			return
		}

		logger.Printf("Processing job %s (%s): %s -> %s", job.ID, job.Type, job.SrcPath, job.DstPath)

		started, err := q.StartJob(job.ID)
		if err != nil {
			logger.Printf("Error updating job %s status: %v", job.ID, err)
			continue
		}
		if !started {
			logger.Printf("Job %s is no longer pending", job.ID)
			continue
		}

		if err := exec.Execute(job); err != nil {
			logger.Printf("Job %s failed: %v", job.ID, err)
			errUpdate := q.UpdateStatus(job.ID, queue.StatusFailed, err.Error())
			if errUpdate != nil {
				logger.Printf("FATAL: Failed to update job %s to failed: %v", job.ID, errUpdate)
			}
			continue
		}

		logger.Printf("Job %s completed", job.ID)
		if errUpdate := q.UpdateStatus(job.ID, queue.StatusCompleted, ""); errUpdate != nil {
			logger.Printf("FATAL: Failed to update job %s to completed: %v", job.ID, errUpdate)
		}
	}
}
