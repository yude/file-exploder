package executor

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yude/file-exploder/server/internal/queue"
)

func randomTempFileName(prefix string) string {
	b := make([]byte, 8)
	rand.Read(b)
	return prefix + "." + hex.EncodeToString(b) + ".tmp"
}

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
	default:
		return fmt.Errorf("unknown job type: %s", job.Type)
	}
}

func (e *Executor) executeRename(job *queue.Job) error {
	if err := validatePaths(job.SrcPath, job.DstPath); err != nil {
		return err
	}
	// Same-path short-circuit
	if filepath.Clean(job.SrcPath) == filepath.Clean(job.DstPath) {
		return nil
	}
	
	if _, err := os.Lstat(job.DstPath); err == nil {
		return fmt.Errorf("destination path already exists: %s", job.DstPath)
	}
	// For renaming across devices, fallback to copy+delete
	err := os.Rename(job.SrcPath, job.DstPath)
	if err != nil {
		if strings.Contains(err.Error(), "cross-device link") || strings.Contains(err.Error(), "invalid cross-device link") {
			if copyErr := copyPath(job.SrcPath, job.DstPath); copyErr != nil {
				return fmt.Errorf("cross-device rename failed during copy: %v", copyErr)
			}
			if delErr := os.RemoveAll(job.SrcPath); delErr != nil {
				return fmt.Errorf("cross-device rename failed to delete source: %v", delErr)
			}
			return nil
		}
		return err
	}
	return nil
}

func (e *Executor) executeDelete(job *queue.Job) error {
	if job.SrcPath == "" {
		return fmt.Errorf("source path is required for delete")
	}
	if err := validatePaths(job.SrcPath); err != nil {
		return err
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
	if err := validatePaths(job.DstPath); err != nil {
		return err
	}
	
	// Create with restrictive permissions first
	return os.MkdirAll(job.DstPath, 0700)
}

func (e *Executor) executeChmod(job *queue.Job) error {
	if job.DstPath == "" {
		return fmt.Errorf("destination path is required for chmod")
	}
	if err := validatePaths(job.DstPath); err != nil {
		return err
	}
	if job.Mode == "" {
		return fmt.Errorf("mode is required for chmod")
	}
	mode, err := parseFileMode(job.Mode)
	if err != nil {
		return err
	}
	// Verify target exists
	if _, err := os.Lstat(job.DstPath); err != nil {
		return err
	}
	return os.Chmod(job.DstPath, mode)
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
		if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
			return fmt.Errorf("path cannot contain relative parent references: %s", p)
		}
		// Also block absolute paths that resolve to system roots even if indirectly
		if cleaned == "/bin" || cleaned == "/boot" || cleaned == "/dev" || cleaned == "/etc" || cleaned == "/lib" || cleaned == "/lib64" || cleaned == "/proc" || cleaned == "/root" || cleaned == "/run" || cleaned == "/sbin" || cleaned == "/sys" || cleaned == "/usr" || cleaned == "/var" {
			return fmt.Errorf("path cannot be a system directory: %s", p)
		}
	}
	return nil
}

func copyPath(src, dst string) error {
	cleanedSrc := filepath.Clean(src)
	cleanedDst := filepath.Clean(dst)
	if cleanedSrc == cleanedDst {
		return fmt.Errorf("source and destination are the same path")
	}
	if strings.HasPrefix(cleanedDst, cleanedSrc+string(filepath.Separator)) {
		return fmt.Errorf("cannot copy a directory into itself")
	}

	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return err
	}

	if srcInfo.Mode()&os.ModeSymlink != 0 {
		link, err := os.Readlink(src)
		if err != nil {
			return err
		}
		if _, err := os.Lstat(dst); err == nil {
			return fmt.Errorf("destination already exists: %s", dst)
		}
		return os.Symlink(link, dst)
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	dstTmp := randomTempFileName(dst)
	dstFile, err := os.OpenFile(dstTmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer func() {
		dstFile.Close()
		os.Remove(dstTmp) // Clean up temp file if rename fails or panic
	}()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	dstFile.Close() // Explicitly close before rename

	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("destination already exists: %s", dst)
	}

	return os.Rename(dstTmp, dst)
}

func copyDir(src, dst string) error {
	// First gather everything to copy to verify it won't fail midway (as best we can)
	// and to ensure we don't pick up files we create during the copy
	var entries []struct {
		relPath string
		info    os.FileInfo
	}
	
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if relPath != "." { // skip the root dir itself
			entries = append(entries, struct {
				relPath string
				info    os.FileInfo
			}{relPath, info})
		}
		return nil
	})
	if err != nil {
		return err
	}

	srcInfo, err := os.Lstat(src)
	if err != nil {
		return err
	}
	
	// Refuse to merge directories. If the destination already exists, fail.
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("destination already exists: %s", dst)
	}

	// Create root dst
	if err := os.MkdirAll(dst, srcInfo.Mode()|0700); err != nil {
		return err
	}
	
	// Keep track of created dirs to revert on failure
	var createdDirs []string
	createdDirs = append(createdDirs, dst)
	
	defer func() {
		if err != nil {
			for i := len(createdDirs) - 1; i >= 0; i-- {
				os.RemoveAll(createdDirs[i])
			}
		}
	}()
	
	// Create all directories first
	for _, entry := range entries {
		if entry.info.IsDir() {
			dstPath := filepath.Join(dst, entry.relPath)
			if errMkdir := os.MkdirAll(dstPath, entry.info.Mode()|0700); errMkdir != nil {
				err = errMkdir
				return err
			}
			createdDirs = append(createdDirs, dstPath)
		}
	}

	// Then copy files
	for _, entry := range entries {
		if !entry.info.IsDir() {
			srcPath := filepath.Join(src, entry.relPath)
			dstPath := filepath.Join(dst, entry.relPath)
			if errCopy := copyFile(srcPath, dstPath); errCopy != nil {
				err = errCopy
				return err
			}
		}
	}
	
	err = nil
	return nil
}

func parseFileMode(s string) (os.FileMode, error) {
	s = strings.TrimPrefix(s, "0")
	if len(s) == 0 || len(s) > 4 {
		return 0, fmt.Errorf("invalid file mode length: %s", s)
	}
	mode, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid file mode: %s", s)
	}
	return os.FileMode(mode), nil
}
