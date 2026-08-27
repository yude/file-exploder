package queue

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestQueue(t *testing.T) *SQLiteQueue {
	t.Helper()
	q, err := NewSQLiteQueue(filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = q.Close() })
	return q
}

func addTestJob(t *testing.T, q *SQLiteQueue, id string) {
	t.Helper()
	if err := q.AddJob(&Job{
		ID:        id,
		Type:      JobDelete,
		SrcPath:   "/tmp/example",
		Status:    StatusPending,
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestQueueLifecycle(t *testing.T) {
	q := newTestQueue(t)
	addTestJob(t, q, "job-1")

	started, err := q.StartJob("job-1")
	if err != nil || !started {
		t.Fatalf("StartJob() = %v, %v", started, err)
	}
	started, err = q.StartJob("job-1")
	if err != nil || started {
		t.Fatalf("second StartJob() = %v, %v", started, err)
	}

	pending, err := q.GetPendingJobs()
	if err != nil || len(pending) != 0 {
		t.Fatalf("GetPendingJobs() = %d jobs, %v", len(pending), err)
	}
	active, err := q.GetActiveJobs()
	if err != nil || len(active) != 1 || active[0].Status != StatusRunning {
		t.Fatalf("GetActiveJobs() = %#v, %v", active, err)
	}

	if err := q.UpdateStatus("job-1", StatusCompleted, ""); err != nil {
		t.Fatal(err)
	}
	job, err := q.GetJob("job-1")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != StatusCompleted || job.CompletedAt == nil {
		t.Fatalf("completed job = %#v", job)
	}
	if err := q.UpdateStatus("job-1", StatusFailed, "late update"); err == nil {
		t.Fatal("late terminal-state update unexpectedly succeeded")
	}
}

func TestCancelOnlyPendingJob(t *testing.T) {
	q := newTestQueue(t)
	addTestJob(t, q, "pending")
	addTestJob(t, q, "running")
	if started, err := q.StartJob("running"); err != nil || !started {
		t.Fatalf("StartJob() = %v, %v", started, err)
	}

	if err := q.CancelJob("pending"); err != nil {
		t.Fatal(err)
	}
	cancelled, err := q.GetJob("pending")
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != StatusCancelled || cancelled.CompletedAt == nil {
		t.Fatalf("cancelled job = %#v", cancelled)
	}
	if err := q.CancelJob("running"); err == nil {
		t.Fatal("running job cancellation unexpectedly succeeded")
	}
}

func TestResetRunningJobsMarksFailure(t *testing.T) {
	q := newTestQueue(t)
	addTestJob(t, q, "running")
	if started, err := q.StartJob("running"); err != nil || !started {
		t.Fatalf("StartJob() = %v, %v", started, err)
	}
	if err := q.ResetRunningJobs(); err != nil {
		t.Fatal(err)
	}
	job, err := q.GetJob("running")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != StatusFailed || job.CompletedAt == nil || job.Error == "" {
		t.Fatalf("reset job = %#v", job)
	}
}

func TestRecentLogsContainOnlyTerminalJobsAndEncodeAsArray(t *testing.T) {
	q := newTestQueue(t)
	logs, err := q.GetRecentLogs(10)
	if err != nil {
		t.Fatal(err)
	}
	if logs == nil || len(logs) != 0 {
		t.Fatalf("empty logs = %#v", logs)
	}

	addTestJob(t, q, "pending")
	addTestJob(t, q, "completed")
	if started, err := q.StartJob("completed"); err != nil || !started {
		t.Fatalf("StartJob() = %v, %v", started, err)
	}
	if err := q.UpdateStatus("completed", StatusCompleted, ""); err != nil {
		t.Fatal(err)
	}
	logs, err = q.GetRecentLogs(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].ID != "completed" {
		t.Fatalf("logs = %#v", logs)
	}
}

func TestRecentLogsHonoursLimit(t *testing.T) {
	q := newTestQueue(t)
	for _, id := range []string{"first", "second", "third"} {
		addTestJob(t, q, id)
		if started, err := q.StartJob(id); err != nil || !started {
			t.Fatalf("StartJob(%q) = %v, %v", id, started, err)
		}
		if err := q.UpdateStatus(id, StatusCompleted, ""); err != nil {
			t.Fatal(err)
		}
	}

	logs, err := q.GetRecentLogs(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("GetRecentLogs(2) returned %d jobs", len(logs))
	}
}
