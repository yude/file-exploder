package queue

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type SQLiteQueue struct {
	db *sql.DB
}

func NewSQLiteQueue(dbPath string) (*SQLiteQueue, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
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
	_, err := db.Exec(schema)
	return err
}

func (q *SQLiteQueue) AddJob(job *Job) error {
	_, err := q.db.Exec(
		`INSERT INTO jobs (id, type, src_path, dst_path, mode, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.Type, job.SrcPath, job.DstPath, job.Mode, job.Status, job.CreatedAt,
	)
	return err
}

func (q *SQLiteQueue) GetJob(id string) (*Job, error) {
	job := &Job{}
	var startedAt, completedAt sql.NullTime
	var errMsg sql.NullString
	err := q.db.QueryRow(
		`SELECT id, type, src_path, dst_path, mode, status, error, created_at, started_at, completed_at
		 FROM jobs WHERE id = ?`, id,
	).Scan(&job.ID, &job.Type, &job.SrcPath, &job.DstPath, &job.Mode,
		&job.Status, &errMsg, &job.CreatedAt, &startedAt, &completedAt)
	if err != nil {
		return nil, err
	}
	if errMsg.Valid {
		job.Error = errMsg.String
	}
	if startedAt.Valid {
		job.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		job.CompletedAt = &completedAt.Time
	}
	return job, nil
}

func (q *SQLiteQueue) StartJob(id string) (bool, error) {
	now := time.Now()
	result, err := q.db.Exec(`UPDATE jobs SET status='running', started_at=?, error='' WHERE id=? AND status='pending'`, now, id)
	if err != nil {
		return false, err
	}
	rows, _ := result.RowsAffected()
	return rows > 0, nil
}

func (q *SQLiteQueue) UpdateStatus(id string, status JobStatus, errMsg string) error {
	now := time.Now()
	switch status {
	case StatusRunning:
		_, err := q.db.Exec(`UPDATE jobs SET status=?, started_at=?, error='' WHERE id=?`, status, now, id)
		return err
	case StatusCompleted:
		_, err := q.db.Exec(`UPDATE jobs SET status=?, completed_at=?, error='' WHERE id=?`, status, now, id)
		return err
	case StatusFailed:
		_, err := q.db.Exec(`UPDATE jobs SET status=?, completed_at=?, error=? WHERE id=?`, status, now, errMsg, id)
		return err
	default:
		_, err := q.db.Exec(`UPDATE jobs SET status=? WHERE id=?`, status, id)
		return err
	}
}

func (q *SQLiteQueue) GetPendingJobs() ([]*Job, error) {
	return q.queryJobs(`SELECT id, type, src_path, dst_path, mode, status, error, created_at, started_at, completed_at
		FROM jobs WHERE status IN ('pending', 'running') ORDER BY created_at ASC`)
}

func (q *SQLiteQueue) ResetRunningJobs() error {
	_, err := q.db.Exec(`UPDATE jobs SET status = 'failed', error = 'Daemon crashed before completion' WHERE status = 'running'`)
	return err
}

func (q *SQLiteQueue) GetAllJobs() ([]*Job, error) {
	return q.queryJobs(`SELECT id, type, src_path, dst_path, mode, status, error, created_at, started_at, completed_at
		FROM jobs ORDER BY created_at DESC`)
}

func (q *SQLiteQueue) CancelJob(id string) error {
	// First update the status if pending
	result, err := q.db.Exec(`UPDATE jobs SET status='cancelled' WHERE id=? AND status = 'pending'`, id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
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
	return q.queryJobs(fmt.Sprintf(`SELECT id, type, src_path, dst_path, mode, status, error, created_at, started_at, completed_at
		FROM jobs ORDER BY completed_at DESC, created_at DESC LIMIT %d`, limit))
}

func (q *SQLiteQueue) queryJobs(query string, args ...interface{}) ([]*Job, error) {
	rows, err := q.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		job := &Job{}
		var startedAt, completedAt sql.NullTime
		var errMsg sql.NullString
		if err := rows.Scan(&job.ID, &job.Type, &job.SrcPath, &job.DstPath, &job.Mode,
			&job.Status, &errMsg, &job.CreatedAt, &startedAt, &completedAt); err != nil {
			return nil, err
		}
		if errMsg.Valid {
			job.Error = errMsg.String
		}
		if startedAt.Valid {
			job.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			job.CompletedAt = &completedAt.Time
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
