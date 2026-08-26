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
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	if err := migrate(db); err != nil {
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
		FROM jobs WHERE status = 'pending' ORDER BY created_at ASC`)
}

func (q *SQLiteQueue) ResetRunningJobs() error {
	_, err := q.db.Exec(`UPDATE jobs SET status = 'pending', error = 'reset after daemon restart' WHERE status = 'running'`)
	return err
}

func (q *SQLiteQueue) GetAllJobs() ([]*Job, error) {
	return q.queryJobs(`SELECT id, type, src_path, dst_path, mode, status, error, created_at, started_at, completed_at
		FROM jobs ORDER BY created_at DESC`)
}

func (q *SQLiteQueue) CancelJob(id string) error {
	result, err := q.db.Exec(`UPDATE jobs SET status='cancelled' WHERE id=? AND status = 'pending'`, id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("job %s not found or not cancellable (must be pending)", id)
	}
	return nil
}

func (q *SQLiteQueue) GetRecentLogs(limit int) ([]*Job, error) {
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
	return jobs, nil
}

func (q *SQLiteQueue) Close() error {
	return q.db.Close()
}
