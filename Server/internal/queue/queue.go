package queue

import (
	"time"
)

type JobType string

const (
	JobRename JobType = "rename"
	JobMove   JobType = "move"
	JobDelete JobType = "delete"
	JobCopy   JobType = "copy"
	JobMkdir  JobType = "mkdir"
	JobChmod  JobType = "chmod"
)

type JobStatus string

const (
	StatusPending   JobStatus = "pending"
	StatusRunning   JobStatus = "running"
	StatusCompleted JobStatus = "completed"
	StatusFailed    JobStatus = "failed"
	StatusCancelled JobStatus = "cancelled"
)

type Job struct {
	ID          string     `json:"id"`
	Type        JobType    `json:"type"`
	SrcPath     string     `json:"src_path,omitempty"`
	DstPath     string     `json:"dst_path,omitempty"`
	Mode        string     `json:"mode,omitempty"`
	Status      JobStatus  `json:"status"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type Queue interface {
	AddJob(job *Job) error
	GetJob(id string) (*Job, error)
	UpdateStatus(id string, status JobStatus, errMsg string) error
	StartJob(id string) (bool, error)
	GetPendingJobs() ([]*Job, error)
	GetActiveJobs() ([]*Job, error)
	ResetRunningJobs() error
	CancelJob(id string) error
	GetRecentLogs(limit int) ([]*Job, error)
}
