package queue

import (
	"database/sql"
	"fmt"
	"net/url"
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
		Scheme:   "file",
		Path:     dbPath,
		RawQuery: "_journal_mode=WAL&_busy_timeout=5000",
	}
	return dsn.String()
}

func NewSQLiteQueue(dbPath string) (*SQLiteQueue, error) {
	db, err := sql.Open("sqlite3", sqliteDSN(dbPath))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
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
	`
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	return normalizeTimestamps(db)
}

// normalizeTimestamps rewrites timestamps that were stored with a local UTC
// offset into UTC. The driver writes a time.Time using whatever offset it
// carries, and the text comparison behind `ORDER BY completed_at` is only
// chronological while every row shares one offset - so a DST transition, or an
// administrator changing the server's timezone, would otherwise shuffle the job
// history. Rows already in UTC are left alone, which makes this a no-op after
// the first run.
func normalizeTimestamps(db *sql.DB) error {
	type record struct {
		id          string
		createdAt   time.Time
		startedAt   sql.NullTime
		completedAt sql.NullTime
	}

	rows, err := db.Query(`SELECT id, created_at, started_at, completed_at FROM jobs
		WHERE created_at NOT LIKE '%+00:00'
		   OR (started_at IS NOT NULL AND started_at NOT LIKE '%+00:00')
		   OR (completed_at IS NOT NULL AND completed_at NOT LIKE '%+00:00')`)
	if err != nil {
		return err
	}
	var stale []record
	for rows.Next() {
		var r record
		if err := rows.Scan(&r.id, &r.createdAt, &r.startedAt, &r.completedAt); err != nil {
			rows.Close()
			return err
		}
		stale = append(stale, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(stale) == 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, r := range stale {
		if _, err := tx.Exec(`UPDATE jobs SET created_at=?, started_at=?, completed_at=? WHERE id=?`,
			r.createdAt.UTC(), utcOrNull(r.startedAt), utcOrNull(r.completedAt), r.id); err != nil {
			return err
		}
	}
	return tx.Commit()
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

func (q *SQLiteQueue) ResetRunningJobs() error {
	_, err := q.db.Exec(
		`UPDATE jobs SET status='failed', error='daemon stopped before completion', completed_at=? WHERE status='running'`,
		time.Now().UTC(),
	)
	return err
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

func (q *SQLiteQueue) GetRecentLogs(limit int) ([]*Job, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}
	return q.queryJobs(`SELECT `+jobColumns+`
		FROM jobs WHERE status IN ('completed', 'failed', 'cancelled')
		ORDER BY completed_at DESC, created_at DESC LIMIT ?`, limit)
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
