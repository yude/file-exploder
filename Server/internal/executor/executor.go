package executor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/yude/file-exploder/server/internal/queue"
)

type Executor struct {
	q queue.Queue
}

func NewExecutor(q queue.Queue) *Executor {
	return &Executor{q: q}
}

func (e *Executor) Execute(job *queue.Job) error {
	switch job.Type {
	case queue.JobRename, queue.JobMove:
		return e.executeRename(job)
	case queue.JobDelete:
		return e.executeDelete(job)
	case queue.JobCopy:
		return e.executeCopy(job)
	case queue.JobMkdir:
		return e.executeMkdir(job)
	case queue.JobChmod:
		return e.executeChmod(job)
	case queue.JobUpload:
		return e.executeUpload(job)
	case queue.JobDownload:
		return e.executeDownload(job)
	default:
		return fmt.Errorf("unknown job type: %s", job.Type)
	}
}

func (e *Executor) executeRename(job *queue.Job) error {
	if err := validatePaths(job.SrcPath, job.DstPath); err != nil {
		return err
	}
	return os.Rename(job.SrcPath, job.DstPath)
}

func (e *Executor) executeDelete(job *queue.Job) error {
	if job.SrcPath == "" {
		return fmt.Errorf("source path is required for delete")
	}
	info, err := os.Lstat(job.SrcPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return os.RemoveAll(job.SrcPath)
	}
	return os.Remove(job.SrcPath)
}

func (e *Executor) executeCopy(job *queue.Job) error {
	if err := validatePaths(job.SrcPath, job.DstPath); err != nil {
		return err
	}
	return copyPath(job.SrcPath, job.DstPath)
}

func (e *Executor) executeMkdir(job *queue.Job) error {
	if job.DstPath == "" {
		return fmt.Errorf("destination path is required for mkdir")
	}
	return os.MkdirAll(job.DstPath, 0755)
}

func (e *Executor) executeChmod(job *queue.Job) error {
	if job.DstPath == "" {
		return fmt.Errorf("destination path is required for chmod")
	}
	if job.Mode == "" {
		return fmt.Errorf("mode is required for chmod")
	}
	mode, err := parseFileMode(job.Mode)
	if err != nil {
		return err
	}
	return os.Chmod(job.DstPath, mode)
}

func (e *Executor) executeUpload(job *queue.Job) error {
	if err := validatePaths(job.SrcPath, job.DstPath); err != nil {
		return err
	}
	return copyFile(job.SrcPath, job.DstPath)
}

func (e *Executor) executeDownload(job *queue.Job) error {
	if err := validatePaths(job.SrcPath, job.DstPath); err != nil {
		return err
	}
	return copyFile(job.SrcPath, job.DstPath)
}

func validatePaths(paths ...string) error {
	for _, p := range paths {
		if p == "" {
			return fmt.Errorf("path cannot be empty")
		}
		cleaned := filepath.Clean(p)
		if cleaned == "." || cleaned == "/" {
			return fmt.Errorf("path cannot be root or current directory: %s", p)
		}
	}
	return nil
}

func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, srcInfo.Mode())
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}
		return copyFile(path, dstPath)
	})
}

func parseFileMode(s string) (os.FileMode, error) {
	s = strings.TrimPrefix(s, "0")
	var mode uint32
	_, err := fmt.Sscanf(s, "%o", &mode)
	if err != nil {
		return 0, fmt.Errorf("invalid file mode: %s", s)
	}
	return os.FileMode(mode), nil
}
