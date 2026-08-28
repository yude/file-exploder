package cmd

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yude/file-exploder/server/internal/queue"
)

var errFakeExecute = errors.New("fake execute failure")

func newTestJobQueue(t *testing.T) *queue.SQLiteQueue {
	t.Helper()
	q, err := queue.NewSQLiteQueue(filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = q.Close() })
	return q
}

func addAndStartTestJob(t *testing.T, q *queue.SQLiteQueue, id string) *queue.Job {
	t.Helper()
	if err := q.AddJob(&queue.Job{ID: id, Type: queue.JobDelete, SrcPath: "/tmp/x", Status: queue.StatusPending, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if started, err := q.StartJob(id); err != nil || !started {
		t.Fatalf("StartJob(%q) = %v, %v", id, started, err)
	}
	job, err := q.GetJob(id)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

var discardLogger = log.New(io.Discard, "", 0)

func TestRunJobWithTimeoutRecordsANormalCompletion(t *testing.T) {
	q := newTestJobQueue(t)
	job := addAndStartTestJob(t, q, "quick")

	runJobWithTimeout(q, func(*queue.Job) error { return nil }, job, time.Second, discardLogger, newBusyPaths())

	got, err := q.GetJob("quick")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != queue.StatusCompleted {
		t.Fatalf("status = %q, want completed", got.Status)
	}
}

func TestRunJobWithTimeoutRecordsANormalFailure(t *testing.T) {
	q := newTestJobQueue(t)
	job := addAndStartTestJob(t, q, "fails")

	runJobWithTimeout(q, func(*queue.Job) error { return errFakeExecute }, job, time.Second, discardLogger, newBusyPaths())

	got, err := q.GetJob("fails")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != queue.StatusFailed || got.Error != errFakeExecute.Error() {
		t.Fatalf("job = %#v, want failed with %q", got, errFakeExecute.Error())
	}
}

func TestRunJobWithTimeoutAbandonsAHungJobAndIgnoresItsLateResult(t *testing.T) {
	q := newTestJobQueue(t)
	job := addAndStartTestJob(t, q, "hung")

	release := make(chan struct{})
	returned := make(chan struct{})
	execute := func(*queue.Job) error {
		<-release
		close(returned)
		return nil
	}

	start := time.Now()
	runJobWithTimeout(q, execute, job, 20*time.Millisecond, discardLogger, newBusyPaths())
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("runJobWithTimeout waited %s for a hung job instead of giving up at its timeout", elapsed)
	}

	got, err := q.GetJob("hung")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != queue.StatusFailed || !strings.Contains(got.Error, "timed out") {
		t.Fatalf("job = %#v, want failed with a timeout message", got)
	}

	// Let the "hung" goroutine finish and confirm its late result did not
	// clobber the timeout outcome already recorded above.
	close(release)
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("abandoned goroutine never observed the release signal")
	}
	time.Sleep(20 * time.Millisecond)

	got, err = q.GetJob("hung")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != queue.StatusFailed || !strings.Contains(got.Error, "timed out") {
		t.Fatalf("late completion changed the recorded outcome: %#v", got)
	}
}

func TestProcessPendingJobsSkipsAJobOverlappingATimedOutJobsPath(t *testing.T) {
	q := newTestJobQueue(t)

	release := make(chan struct{})
	hungStarted := make(chan struct{})
	var mu sync.Mutex
	var executed []string
	execute := func(job *queue.Job) error {
		mu.Lock()
		executed = append(executed, job.ID)
		mu.Unlock()
		if job.ID == "hung" {
			close(hungStarted)
			<-release
		}
		return nil
	}

	now := time.Now()
	if err := q.AddJob(&queue.Job{ID: "hung", Type: queue.JobDelete, SrcPath: "/data/shared", Status: queue.StatusPending, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := q.AddJob(&queue.Job{ID: "overlaps", Type: queue.JobDelete, SrcPath: "/data/shared/child", Status: queue.StatusPending, CreatedAt: now.Add(time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	if err := q.AddJob(&queue.Job{ID: "unrelated", Type: queue.JobDelete, SrcPath: "/data/other", Status: queue.StatusPending, CreatedAt: now.Add(2 * time.Millisecond)}); err != nil {
		t.Fatal(err)
	}

	busy := newBusyPaths()
	ctx := context.Background()
	processPendingJobs(ctx, q, execute, discardLogger, 20*time.Millisecond, busy)

	select {
	case <-hungStarted:
	default:
		t.Fatal("the hung job's execute was never called")
	}

	mu.Lock()
	gotExecuted := append([]string(nil), executed...)
	mu.Unlock()
	if !slices.Contains(gotExecuted, "unrelated") {
		t.Fatalf("executed = %v, want it to include the unrelated job", gotExecuted)
	}
	if slices.Contains(gotExecuted, "overlaps") {
		t.Fatalf("executed = %v, want the job overlapping the still-running hung job's path to have been skipped", gotExecuted)
	}

	overlapsJob, err := q.GetJob("overlaps")
	if err != nil {
		t.Fatal(err)
	}
	if overlapsJob.Status != queue.StatusPending {
		t.Fatalf("overlapping job status = %q, want it left pending", overlapsJob.Status)
	}

	// Let the abandoned "hung" goroutine actually finish, releasing its hold
	// on /data/shared.
	close(release)
	time.Sleep(20 * time.Millisecond)

	processPendingJobs(ctx, q, execute, discardLogger, time.Second, busy)
	overlapsJob, err = q.GetJob("overlaps")
	if err != nil {
		t.Fatal(err)
	}
	if overlapsJob.Status != queue.StatusCompleted {
		t.Fatalf("overlapping job status after the hung job released its path = %q, want completed", overlapsJob.Status)
	}
}

func TestRotatingLogWriterRotatesDuringContinuousRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.log")
	w, err := newRotatingLogWriter(path, 16)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("first-line")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("second-line")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != "second-line" || string(previous) != "first-line" {
		t.Fatalf("current=%q previous=%q", current, previous)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("log mode = %o, want 600", info.Mode().Perm())
	}
}

func TestRotatingLogWriterRotatesOversizedFileAtOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 17)), 0600); err != nil {
		t.Fatal(err)
	}
	w, err := newRotatingLogWriter(path, 16)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatal(err)
	}
}

func TestRotatingLogWriterKeepsWorkingAfterAFailedRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "queue.log")
	w, err := newRotatingLogWriter(path, 16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })

	if _, err := w.Write([]byte("first-line")); err != nil {
		t.Fatal(err)
	}

	// A directory at the rotation target makes os.Rename fail.
	if err := os.Mkdir(path+".1", 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("second-line")); err == nil {
		t.Fatal("expected the failed rotation to be reported")
	}

	// The writer must have reopened the active log rather than wedging.
	if err := os.RemoveAll(path + ".1"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("third-line")); err != nil {
		t.Fatalf("writer did not recover from a failed rotation: %v", err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current), "third-line") {
		t.Fatalf("log = %q, want it to contain third-line", current)
	}
}

func TestRotatingLogWriterRecoversWhenAReopenFailed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "queue.log")
	w, err := newRotatingLogWriter(path, 16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })

	if _, err := w.Write([]byte("first-line")); err != nil {
		t.Fatal(err)
	}

	// Rotate with the whole directory unwritable: the rename fails and so does
	// the reopen, which used to leave the writer without a handle for good.
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("second-line")); err == nil {
		t.Fatal("expected the failed rotation to be reported")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}

	if _, err := w.Write([]byte("third-line")); err != nil {
		t.Fatalf("writer stayed mute after the directory became writable again: %v", err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current), "third-line") {
		t.Fatalf("log = %q", current)
	}

	// A deliberate Close still means closed.
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("after-close")); err == nil {
		t.Fatal("writing after Close unexpectedly succeeded")
	}
}
