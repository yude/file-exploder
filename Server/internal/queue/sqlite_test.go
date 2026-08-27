package queue

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
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

func TestOpensDatabasesUnderAwkwardDirectoryNames(t *testing.T) {
	// A '?' in the path used to end the filename as far as the driver was
	// concerned, so the queue quietly lived somewhere else entirely.
	dir := filepath.Join(t.TempDir(), "data?dir #1")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "queue.db")

	q, err := NewSQLiteQueue(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteQueue: %v", err)
	}
	t.Cleanup(func() { _ = q.Close() })

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("database was not created at the requested path: %v", err)
	}
	var journalMode string
	if err := q.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}
}

// storedTimestamps reads the raw text SQLite holds, bypassing the driver's
// time.Time conversion, so the on-disk representation can be asserted directly.
func storedTimestamps(t *testing.T, q *SQLiteQueue, id string) (string, string) {
	t.Helper()
	var createdAt string
	var completedAt sql.NullString
	if err := q.db.QueryRow(
		`SELECT CAST(created_at AS TEXT), CAST(completed_at AS TEXT) FROM jobs WHERE id = ?`, id,
	).Scan(&createdAt, &completedAt); err != nil {
		t.Fatal(err)
	}
	return createdAt, completedAt.String
}

func TestTimestampsAreStoredInUTC(t *testing.T) {
	q := newTestQueue(t)
	tokyo := time.FixedZone("JST", 9*60*60)
	created := time.Date(2026, 8, 27, 12, 51, 39, 606559075, tokyo)

	if err := q.AddJob(&Job{ID: "job", Type: JobDelete, SrcPath: "/tmp/x", Status: StatusPending, CreatedAt: created}); err != nil {
		t.Fatal(err)
	}
	if started, err := q.StartJob("job"); err != nil || !started {
		t.Fatalf("StartJob() = %v, %v", started, err)
	}
	if err := q.UpdateStatus("job", StatusCompleted, ""); err != nil {
		t.Fatal(err)
	}

	createdAt, completedAt := storedTimestamps(t, q, "job")
	for label, value := range map[string]string{"created_at": createdAt, "completed_at": completedAt} {
		if !strings.HasSuffix(value, "+00:00") {
			t.Errorf("%s stored as %q, want a UTC offset", label, value)
		}
	}

	// The instant itself must survive the conversion.
	job, err := q.GetJob("job")
	if err != nil {
		t.Fatal(err)
	}
	if !job.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want the same instant as %v", job.CreatedAt, created)
	}
}

func TestMigrationRewritesLocalTimestampsAsUTC(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "queue.db")
	q, err := NewSQLiteQueue(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	// Rows exactly as a pre-upgrade daemon left them: local time plus offset.
	// "later" is the more recent instant but sorts *earlier* as text, which is
	// the misordering this migration exists to remove.
	earlier := "2026-11-01 01:30:00.5+02:00" // 23:30 UTC on Oct 31
	later := "2026-11-01 01:00:00.5+01:00"   // 00:00 UTC on Nov 1
	for id, stamp := range map[string]string{"earlier": earlier, "later": later} {
		if _, err := q.db.Exec(
			`INSERT INTO jobs (id, type, src_path, status, created_at, started_at, completed_at)
			 VALUES (?, 'delete', '/tmp/x', 'completed', ?, ?, ?)`, id, stamp, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopening runs the migration.
	q, err = NewSQLiteQueue(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = q.Close() })

	for _, id := range []string{"earlier", "later"} {
		createdAt, completedAt := storedTimestamps(t, q, id)
		if !strings.HasSuffix(createdAt, "+00:00") || !strings.HasSuffix(completedAt, "+00:00") {
			t.Fatalf("%s still stored with a local offset: %q / %q", id, createdAt, completedAt)
		}
	}

	logs, err := q.GetRecentLogs(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("GetRecentLogs returned %d jobs", len(logs))
	}
	if logs[0].ID != "later" {
		t.Fatalf("most recent job = %q, want \"later\"", logs[0].ID)
	}
}

func TestJobsWithNullOptionalColumnsStillScan(t *testing.T) {
	q := newTestQueue(t)
	// The schema allows NULL in these columns, so one row written without them
	// must not take out every query that touches the table.
	if _, err := q.db.Exec(
		`INSERT INTO jobs (id, type, src_path, dst_path, mode, status, error, created_at)
		 VALUES ('sparse', 'delete', NULL, NULL, NULL, 'pending', NULL, ?)`, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	job, err := q.GetJob("sparse")
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if job.SrcPath != "" || job.DstPath != "" || job.Mode != "" || job.Error != "" {
		t.Fatalf("NULL columns did not scan as empty: %#v", job)
	}

	pending, err := q.GetPendingJobs()
	if err != nil {
		t.Fatalf("GetPendingJobs: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("GetPendingJobs returned %d jobs", len(pending))
	}
}
