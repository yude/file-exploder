package cmd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
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
	busy := newBusyPaths()

	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	logger.Printf("Daemon started, processing queue every %s", pollInterval)

	for {
		select {
		case <-ctx.Done():
			logger.Println("Shutdown signal received, stopping")
			return nil
		case <-ticker.C:
			// Keep going while a pass is still finding work to start. Each
			// pass reads the pending list once, so anything queued *while*
			// that pass was busy executing - which is exactly what a bulk
			// operation looks like, one `add` per file racing the daemon -
			// would otherwise sit out a full tick it has nothing to do with.
			// A pass reports true only when it actually started a job, so
			// this cannot spin: every extra round is one the queue paid for.
			for processPendingJobs(ctx, q, exec.Execute, logger, cfg.JobTimeout, busy) {
				if ctx.Err() != nil {
					break
				}
			}
		}
	}
}

// pollInterval is how long the daemon waits between passes over the queue
// when the last one found nothing to do. It is the floor on how long a
// freshly queued operation waits before anything looks at it, and the clients
// sit on a spinner for exactly that long on every rename, delete and mkdir -
// so it is worth keeping short. One idle pass is a single indexed SQLite
// SELECT that returns no rows, measured at ~30us: four of them a second is
// not a cost worth trading that latency for.
const pollInterval = 250 * time.Millisecond

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

// processPendingJobs runs one pass over the pending queue, reporting whether
// it started any job at all - so the caller can tell a pass that drained real
// work (and may have more waiting behind it) from one that found the queue
// idle. A job left pending because it overlaps a timed-out job's paths, or
// one another writer claimed first, is not work this pass did.
func processPendingJobs(ctx context.Context, q queue.Queue, execute func(*queue.Job) error, logger *log.Logger, timeout time.Duration, busy *busyPaths) bool {
	jobs, err := q.GetPendingJobs()
	if err != nil {
		logger.Printf("Error fetching pending jobs: %v", err)
		return false
	}

	startedAny := false
	for _, job := range jobs {
		// Stop claiming work as soon as a shutdown is requested. The job
		// already in flight still runs to completion; the rest stay pending
		// for the next start instead of being reset to failed.
		if ctx.Err() != nil {
			logger.Printf("Shutdown requested, leaving job %s pending", job.ID)
			return startedAny
		}

		// A job a previous tick gave up on for exceeding its time budget may
		// still be running in the background (see runJobWithTimeout) - the
		// daemon otherwise never has two jobs touching the filesystem at
		// once, so starting a new job that overlaps one of those paths would
		// race with it. Leave it pending; it becomes eligible again as soon
		// as that goroutine actually finishes.
		if paths := jobPaths(job); busy.overlaps(paths) {
			logger.Printf("Job %s touches a path a timed-out job may still be using; leaving it pending", job.ID)
			continue
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

		startedAny = true
		runJobWithTimeout(q, execute, job, timeout, logger, busy)
	}
	return startedAny
}

// runJobWithTimeout runs execute(job) on its own goroutine and gives up on it
// - marking it failed and returning control to the caller - if it does not
// finish within timeout. Execute has no cancellation of its own (a blocked
// syscall on an unresponsive mount cannot be interrupted from Go), so a
// timed-out job's goroutine is left to finish in the background rather than
// leaked forever: if it eventually returns, its outcome is logged but not
// recorded, since finishRunningJob's own status='running' guard would reject
// a write to a job this function has already marked terminal. Its paths are
// held in busy until it does, so processPendingJobs can avoid starting a new
// job that would race with it.
func runJobWithTimeout(q queue.Queue, execute func(*queue.Job) error, job *queue.Job, timeout time.Duration, logger *log.Logger, busy *busyPaths) {
	done := make(chan error, 1)
	go func() { done <- execute(job) }()

	// NewTimer + Stop, not time.After: the job budget is 24h by default, and
	// a time.After timer stays armed in the runtime's timer heap for its full
	// duration however quickly the job actually finishes. Every job the daemon
	// ran would leave one behind for a day - a slow, steady accumulation in a
	// process that is meant to stay up indefinitely.
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case err := <-done:
		if err != nil {
			logger.Printf("Job %s failed: %v", job.ID, err)
			recordTerminalStatus(q, job.ID, queue.StatusFailed, err.Error(), logger)
			return
		}
		logger.Printf("Job %s completed", job.ID)
		recordTerminalStatus(q, job.ID, queue.StatusCompleted, "", logger)
	case <-timer.C:
		logger.Printf("Job %s exceeded its %s execution budget; marking it failed and moving on, but it may still be running in the background", job.ID, timeout)
		recordTerminalStatus(q, job.ID, queue.StatusFailed, fmt.Sprintf("timed out after %s", timeout), logger)
		busy.hold(job.ID, jobPaths(job))
		go func() {
			defer busy.release(job.ID)
			if err := <-done; err != nil {
				logger.Printf("Job %s (already marked as timed out) eventually failed in the background: %v", job.ID, err)
			} else {
				logger.Printf("Job %s (already marked as timed out) eventually completed in the background", job.ID)
			}
		}()
	}
}

// busyPaths tracks the src/dst paths of jobs runJobWithTimeout has given up
// on but whose goroutine is still running in the background. Before that
// timeout mechanism existed, the daemon's single-worker loop guaranteed only
// one job's filesystem work was ever in flight; an abandoned goroutine breaks
// that guarantee on its own, so processPendingJobs consults this to avoid
// starting a new job that could race with one still running.
type busyPaths struct {
	mu    sync.Mutex
	byJob map[string][]string
}

func newBusyPaths() *busyPaths {
	return &busyPaths{byJob: make(map[string][]string)}
}

func (b *busyPaths) hold(jobID string, paths []string) {
	if len(paths) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.byJob[jobID] = paths
}

func (b *busyPaths) release(jobID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.byJob, jobID)
}

// overlaps reports whether any path currently held by an abandoned job is the
// same as, a filesystem ancestor of, or a descendant of any path in paths.
func (b *busyPaths) overlaps(paths []string) bool {
	if len(paths) == 0 {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, held := range b.byJob {
		for _, h := range held {
			for _, p := range paths {
				if pathsOverlap(h, p) {
					return true
				}
			}
		}
	}
	return false
}

// jobPaths returns the paths a job touches, cleaned so they compare
// consistently with pathsOverlap (defined in add.go) regardless of how the
// path was originally spelled.
func jobPaths(job *queue.Job) []string {
	var paths []string
	if job.SrcPath != "" {
		paths = append(paths, filepath.Clean(job.SrcPath))
	}
	if job.DstPath != "" {
		paths = append(paths, filepath.Clean(job.DstPath))
	}
	return paths
}

// terminalStatusAttempts bounds the retries below.
const terminalStatusAttempts = 3

// recordTerminalStatus writes the outcome of a finished job, retrying a failed
// write before giving up.
//
// The work is already done by this point, and the row is the only record of it.
// A row left 'running' is picked up by nothing: GetPendingJobs skips it, cancel
// refuses it, and the client's stalled-queue detection treats anything running
// as progress - so waitForJob waits on it without end, for an operation that
// actually succeeded. Only a daemon restart clears it, and then it is reported
// as failed.
//
// A write here should not fail; if it does - a full disk under the queue, an
// I/O error - one more attempt is worth far more than the alternative.
func recordTerminalStatus(q queue.Queue, id string, status queue.JobStatus, errMsg string, logger *log.Logger) {
	var err error
	for attempt := 1; attempt <= terminalStatusAttempts; attempt++ {
		if err = q.UpdateStatus(id, status, errMsg); err == nil {
			if attempt > 1 {
				logger.Printf("Recorded job %s as %s on attempt %d", id, status, attempt)
			}
			return
		}
		if attempt < terminalStatusAttempts {
			time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
		}
	}
	logger.Printf("FATAL: could not record job %s as %s after %d attempts, so it stays running and nothing will pick it up: %v",
		id, status, terminalStatusAttempts, err)
}
