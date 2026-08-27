package cmd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
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
	Args:  cobra.NoArgs,
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

	logWriter, err := newRotatingLogWriter(cfg.LogPath, maxLogBytes)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer logWriter.Close()

	logger := log.New(logWriter, "[file-exploder] ", log.LstdFlags|log.Lshortfile)
	logger.Println("Daemon starting...")

	q, err := queue.NewSQLiteQueue(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("failed to open queue: %w", err)
	}
	defer q.Close()

	interrupted, err := q.ResetRunningJobs()
	if err != nil {
		// Worth spelling out: jobs left running by the previous daemon then stay
		// running. Nothing picks them up, cancel refuses them, and the client's
		// dead-queue detection keeps seeing something "running" forever, so it
		// waits on operations that will never finish.
		logger.Printf("Failed to reset running jobs, so any job interrupted by the previous daemon stays stuck: %v", err)
	}
	for _, job := range interrupted {
		removed, err := executor.RemoveOrphanedStaging(job)
		for _, path := range removed {
			logger.Printf("Removed staging left by interrupted job %s: %s", job.ID, path)
		}
		if err != nil {
			logger.Printf("Failed to clean staging for job %s: %v", job.ID, err)
		}
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

// maxLogBytes bounds each queue.log generation. One previous generation is
// retained for diagnosis.
const maxLogBytes = 8 << 20

type rotatingLogWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	file     *os.File
	size     int64
	closed   bool
}

func newRotatingLogWriter(path string, maxBytes int64) (*rotatingLogWriter, error) {
	w := &rotatingLogWriter{path: path, maxBytes: maxBytes}
	if info, err := os.Stat(path); err == nil && info.Size() >= maxBytes {
		if err := os.Rename(path, path+".1"); err != nil {
			return nil, err
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *rotatingLogWriter) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	w.file = f
	w.size = info.Size()
	return nil
}

func (w *rotatingLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	if w.file == nil {
		// A rotation left us without a handle because the reopen failed. Try
		// again rather than staying mute for the rest of the daemon's life:
		// log.Logger discards write errors, so that silence reaches nobody.
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	if w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingLogWriter) rotate() error {
	// Drop the handle whatever Close reports. Returning early on a Close error
	// used to leave w.file non-nil and closed, so every later Write failed and
	// retried the same rotation - the daemon stopped logging for good.
	closeErr := w.file.Close()
	w.file = nil
	if err := os.Rename(w.path, w.path+".1"); err != nil {
		// Reopen the active path so a transient rotation failure does not leave
		// the daemon permanently unable to log subsequent work.
		return errors.Join(closeErr, err, w.open())
	}
	return errors.Join(closeErr, w.open())
}

func (w *rotatingLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
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
