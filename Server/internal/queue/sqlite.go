package queue

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// jobColumns is the projection every job query shares, in the order scanJob
// expects.
const jobColumns = `id, type, src_path, dst_path, mode, status, error, created_at, started_at, completed_at`

type SQLiteQueue struct {
	db *sql.DB
}

// sqliteDSN builds the connection string for dbPath. Appending the parameters
// to a bare path is not safe: go-sqlite3 splits such a DSN at the first '?', so
// a data directory containing one opens a different file entirely - silently,
// with a fresh empty queue. The file: URI form percent-encodes the path.
func sqliteDSN(dbPath string) string {
	dsn := url.URL{
		Scheme: "file",
		Path:   dbPath,
		// _txlock=immediate takes the write lock when a transaction opens
		// instead of promoting a read transaction to a write one. Promotion
		// fails with SQLITE_BUSY_SNAPSHOT if anything committed since the read
		// began, and SQLite deliberately does not invoke the busy handler for
		// that - so _busy_timeout does not apply and the transaction dies
		// immediately rather than waiting its turn.
		RawQuery: "_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate",
	}
	return dsn.String()
}

func NewSQLiteQueue(dbPath string) (*SQLiteQueue, error) {
	db, err := sql.Open("sqlite3", sqliteDSN(dbPath))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", errors.Join(err, db.Close()))
	}

	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", errors.Join(err, db.Close()))
	}

	return &SQLiteQueue{db: db}, nil
}

func migrate(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS jobs (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		src_path TEXT,
		dst_path TEXT,
		mode TEXT,
		status TEXT DEFAULT 'pending',
		error TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		started_at DATETIME,
		completed_at DATETIME
	);
	CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
	CREATE INDEX IF NOT EXISTS idx_jobs_terminal ON jobs(status, completed_at DESC, created_at DESC);
	`
	if _, err := db.Exec(schema); err != nil {
		return err
	}

	// normalizeTimestamps used to run unconditionally here, which meant every
	// CLI invocation and daemon start scanned the entire, never-pruned jobs
	// table looking for rows to fix - most of which, after the first run, have
	// nothing to fix. user_version records that the scan has already found and
	// converted every stale row, so later opens skip straight past it.
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version >= 1 {
		return nil
	}
	converged, err := normalizeTimestamps(db)
	if err != nil {
		return err
	}
	if !converged {
		// A row changed under us mid-migration (see normalizeTimestamps); leave
		// the version at 0 so the next open tries again for whatever is left.
		return nil
	}
	_, err = db.Exec(`PRAGMA user_version = 1`)
	return err
}

// normalizeTimestamps rewrites timestamps that were stored with a local UTC
// offset into UTC. The driver writes a time.Time using whatever offset it
// carries, and the text comparison behind `ORDER BY completed_at` is only
// chronological while every row shares one offset - so a DST transition, or an
// administrator changing the server's timezone, would otherwise shuffle the job
// history. Rows already in UTC are left alone, which makes this a no-op after
// the first run.
//
// The reported bool is false if some row's UPDATE didn't apply - a concurrent
// StartJob/UpdateStatus changed started_at/completed_at between the SELECT
// above and this row's turn in the loop - in which case the caller must not
// mark migration as done, or that row would never be revisited.
func normalizeTimestamps(db *sql.DB) (bool, error) {
	stale, err := staleTimestampRows(db)
	if err != nil {
		return false, err
	}
	if len(stale) == 0 {
		return true, nil
	}

	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	converged, err := applyNormalizedTimestamps(tx, stale)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return converged, nil
}

type staleTimestamp struct {
	id          string
	createdAt   time.Time
	startedAt   sql.NullTime
	completedAt sql.NullTime
	// raw* hold the exact text currently in the column, alongside the
	// driver-parsed values above. The parsed time.Time round-trips through the
	// driver's own formatting when written back, which does not necessarily
	// reproduce an arbitrary legacy string byte-for-byte - a pre-migration row
	// is exactly such an arbitrary string, written by whatever earlier version
	// of this program (or something else entirely) put it there. Only the raw
	// text is safe to compare against the row's current contents to detect a
	// concurrent write.
	rawStartedAt, rawCompletedAt sql.NullString
}

func staleTimestampRows(db *sql.DB) ([]staleTimestamp, error) {
	rows, err := db.Query(`SELECT id, created_at, started_at, completed_at,
			CAST(started_at AS TEXT), CAST(completed_at AS TEXT)
		FROM jobs
		WHERE created_at NOT LIKE '%+00:00'
		   OR (started_at IS NOT NULL AND started_at NOT LIKE '%+00:00')
		   OR (completed_at IS NOT NULL AND completed_at NOT LIKE '%+00:00')`)
	if err != nil {
		return nil, err
	}
	var stale []staleTimestamp
	for rows.Next() {
		var r staleTimestamp
		if err := rows.Scan(&r.id, &r.createdAt, &r.startedAt, &r.completedAt,
			&r.rawStartedAt, &r.rawCompletedAt); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		stale = append(stale, r)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Join(err, rows.Close())
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return stale, nil
}

// applyNormalizedTimestamps writes stale's rows back with UTC timestamps. The
// reported bool is false if some row's UPDATE didn't apply - a concurrent
// StartJob/UpdateStatus changed started_at/completed_at between the read that
// produced stale and this call - in which case the caller must not mark
// migration as done, or that row would never be revisited.
func applyNormalizedTimestamps(tx *sql.Tx, stale []staleTimestamp) (bool, error) {
	converged := true
	for _, r := range stale {
		// Guarding on the exact started_at/completed_at text just read makes
		// this a compare-and-swap: if StartJob or UpdateStatus wrote a new
		// value for this row in between, the WHERE no longer matches and the
		// UPDATE becomes a no-op instead of clobbering that concurrent write
		// back to its stale, pre-migration value.
		result, err := tx.Exec(
			`UPDATE jobs SET created_at=?, started_at=?, completed_at=?
			 WHERE id=? AND CAST(started_at AS TEXT) IS ? AND CAST(completed_at AS TEXT) IS ?`,
			r.createdAt.UTC(), utcOrNull(r.startedAt), utcOrNull(r.completedAt),
			r.id, nullStringAny(r.rawStartedAt), nullStringAny(r.rawCompletedAt))
		if err != nil {
			return false, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return false, err
		}
		if affected == 0 {
			converged = false
		}
	}
	return converged, nil
}

func nullStringAny(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func utcOrNull(value sql.NullTime) any {
	if !value.Valid {
		return nil
	}
	return value.Time.UTC()
}

func (q *SQLiteQueue) AddJob(job *Job) error {
	_, err := q.db.Exec(
		`INSERT INTO jobs (id, type, src_path, dst_path, mode, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.Type, job.SrcPath, job.DstPath, job.Mode, job.Status, job.CreatedAt.UTC(),
	)
	return err
}

func (q *SQLiteQueue) GetJob(id string) (*Job, error) {
	return scanJob(q.db.QueryRow(`SELECT `+jobColumns+` FROM jobs WHERE id = ?`, id))
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanJob(row rowScanner) (*Job, error) {
	job := &Job{}
	var startedAt, completedAt sql.NullTime
	// src_path, dst_path and mode are optional per job type and the schema puts
	// no NOT NULL on them, so a row carrying a NULL rather than '' has to scan
	// cleanly. Reading them into plain strings failed the whole query - which
	// meant one such row blinded every listing *and* stopped the daemon picking
	// up any work, not just that job.
	var srcPath, dstPath, mode, errMsg sql.NullString
	if err := row.Scan(&job.ID, &job.Type, &srcPath, &dstPath, &mode,
		&job.Status, &errMsg, &job.CreatedAt, &startedAt, &completedAt); err != nil {
		return nil, err
	}
	job.SrcPath = srcPath.String
	job.DstPath = dstPath.String
	job.Mode = mode.String
	job.Error = errMsg.String
	if startedAt.Valid {
		job.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		job.CompletedAt = &completedAt.Time
	}
	return job, nil
}

func (q *SQLiteQueue) StartJob(id string) (bool, error) {
	now := time.Now().UTC()
	result, err := q.db.Exec(`UPDATE jobs SET status='running', started_at=?, error='' WHERE id=? AND status='pending'`, now, id)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (q *SQLiteQueue) UpdateStatus(id string, status JobStatus, errMsg string) error {
	now := time.Now().UTC()
	switch status {
	case StatusRunning:
		started, err := q.StartJob(id)
		if err != nil {
			return err
		}
		if !started {
			return fmt.Errorf("job %s is no longer pending", id)
		}
		return nil
	case StatusCompleted:
		return q.finishRunningJob(id, status, "", now)
	case StatusFailed:
		return q.finishRunningJob(id, status, errMsg, now)
	default:
		return fmt.Errorf("unsupported status transition to %s", status)
	}
}

func (q *SQLiteQueue) finishRunningJob(id string, status JobStatus, errMsg string, completedAt time.Time) error {
	result, err := q.db.Exec(
		`UPDATE jobs SET status=?, completed_at=?, error=? WHERE id=? AND status='running'`,
		status, completedAt.UTC(), errMsg, id,
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("job %s is no longer running", id)
	}
	return nil
}

func (q *SQLiteQueue) GetPendingJobs() ([]*Job, error) {
	return q.queryJobs(`SELECT ` + jobColumns + ` FROM jobs WHERE status = 'pending' ORDER BY created_at ASC`)
}

func (q *SQLiteQueue) GetActiveJobs() ([]*Job, error) {
	return q.queryJobs(`SELECT ` + jobColumns + ` FROM jobs WHERE status IN ('pending', 'running') ORDER BY created_at ASC`)
}

// ResetRunningJobs fails every job left mid-flight by a previous daemon and
// returns them, so the caller can clean up whatever they were part-way through
// writing.
func (q *SQLiteQueue) ResetRunningJobs() ([]*Job, error) {
	tx, err := q.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT ` + jobColumns + ` FROM jobs WHERE status = 'running'`)
	if err != nil {
		return nil, err
	}
	var interrupted []*Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		interrupted = append(interrupted, job)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Join(err, rows.Close())
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(
		`UPDATE jobs SET status='failed', error='daemon stopped before completion', completed_at=? WHERE status='running'`,
		time.Now().UTC(),
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return interrupted, nil
}

func (q *SQLiteQueue) CancelJob(id string) error {
	// First update the status if pending
	result, err := q.db.Exec(
		`UPDATE jobs SET status='cancelled', completed_at=? WHERE id=? AND status='pending'`,
		time.Now().UTC(), id,
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows > 0 {
		return nil // Successfully cancelled pending job
	}

	// For running jobs, we cannot safely cancel via DB update alone because the
	// executor process is already acting on files (os.Rename, os.MkdirAll, io.Copy etc).
	// Writing 'cancelled' to the DB while it's running will just result in the executor
	// overwriting it with 'completed' or 'failed' moments later.
	// Actual cancellation of running operations requires context.Context threading through
	// the executor, which is not currently implemented.
	return fmt.Errorf("job %s not found or is already running/finished (running jobs cannot be interrupted)", id)
}

var terminalStatuses = []JobStatus{StatusCompleted, StatusFailed, StatusCancelled}

func (q *SQLiteQueue) GetRecentLogs(limit int) ([]*Job, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}

	// idx_jobs_terminal covers (status, completed_at DESC, created_at DESC), so
	// a plain `status = ?` query satisfies this ORDER BY straight from the
	// index. Querying `status IN (...)` instead - the obvious one-query
	// version - makes SQLite fall back to "USE TEMP B-TREE FOR ORDER BY" even
	// with that same index in place, sorting the entire terminal-job history on
	// every call. Querying each status separately (already sorted and capped by
	// the index) and merging the at-most-3*limit rows here keeps the work
	// bounded by limit instead of by total history size.
	merged := make([]*Job, 0)
	for _, status := range terminalStatuses {
		jobs, err := q.queryJobs(`SELECT `+jobColumns+`
			FROM jobs WHERE status = ?
			ORDER BY completed_at DESC, created_at DESC LIMIT ?`, status, limit)
		if err != nil {
			return nil, err
		}
		merged = append(merged, jobs...)
	}
	sort.Slice(merged, func(i, j int) bool { return jobMoreRecent(merged[i], merged[j]) })
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

// jobMoreRecent reports whether a sorts before b under the same
// "completed_at DESC, created_at DESC" order GetRecentLogs' per-status queries
// use, treating a NULL completed_at (not expected for a terminal job, but not
// guaranteed by the schema) as older than any set value.
func jobMoreRecent(a, b *Job) bool {
	switch {
	case a.CompletedAt == nil && b.CompletedAt == nil:
		return a.CreatedAt.After(b.CreatedAt)
	case a.CompletedAt == nil:
		return false
	case b.CompletedAt == nil:
		return true
	case !a.CompletedAt.Equal(*b.CompletedAt):
		return a.CompletedAt.After(*b.CompletedAt)
	default:
		return a.CreatedAt.After(b.CreatedAt)
	}
}

func (q *SQLiteQueue) queryJobs(query string, args ...interface{}) ([]*Job, error) {
	rows, err := q.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := make([]*Job, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (q *SQLiteQueue) Close() error {
	return q.db.Close()
}
