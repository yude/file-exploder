package cmd

import (
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

	f, err := os.OpenFile(cfg.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
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

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	logger.Println("Daemon started, processing queue every 1s")

	for {
		select {
		case sig := <-sigCh:
			logger.Printf("Received signal %v, shutting down", sig)
			return nil
		case <-ticker.C:
			processPendingJobs(q, exec, logger)
		}
	}
}

func processPendingJobs(q queue.Queue, exec *executor.Executor, logger *log.Logger) {
	jobs, err := q.GetPendingJobs()
	if err != nil {
		logger.Printf("Error fetching pending jobs: %v", err)
		return
	}

	for _, job := range jobs {
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
			_ = q.UpdateStatus(job.ID, queue.StatusFailed, err.Error())
			continue
		}

		logger.Printf("Job %s completed", job.ID)
		_ = q.UpdateStatus(job.ID, queue.StatusCompleted, "")
	}
}
