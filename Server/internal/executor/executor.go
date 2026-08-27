package executor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/yude/file-exploder/server/internal/queue"
	"golang.org/x/sys/unix"
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
	default:
		return fmt.Errorf("unknown job type: %s", job.Type)
	}
}

func (e *Executor) executeRename(job *queue.Job) error {
	if err := validatePaths(job.SrcPath, job.DstPath); err != nil {
		return err
	}
	if filepath.Clean(job.SrcPath) == filepath.Clean(job.DstPath) {
		_, err := os.Lstat(job.SrcPath)
		return err
	}

	err := renameNoReplace(job.SrcPath, job.DstPath)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("destination path already exists: %s", job.DstPath)
		}
		if errors.Is(err, syscall.EXDEV) {
			if copyErr := copyPath(job.SrcPath, job.DstPath); copyErr != nil {
				return fmt.Errorf("cross-device rename failed during copy: %w", copyErr)
			}
			if delErr := os.RemoveAll(job.SrcPath); delErr != nil {
				return fmt.Errorf("cross-device rename copied the destination but failed to delete the source: %w", delErr)
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
	return os.MkdirAll(job.DstPath, 0755)
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
	info, err := os.Lstat(job.DstPath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to chmod a symbolic link: %s", job.DstPath)
	}
	return os.Chmod(job.DstPath, mode)
}

func validatePaths(paths ...string) error {
	for _, p := range paths {
		if p == "" {
			return fmt.Errorf("path cannot be empty")
		}
		if !filepath.IsAbs(p) {
			return fmt.Errorf("path must be absolute: %s", p)
		}
		cleaned := filepath.Clean(p)
		if cleaned == "." || cleaned == "/" {
			return fmt.Errorf("path cannot be root or current directory: %s", p)
		}
		if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
			return fmt.Errorf("path cannot contain relative parent references: %s", p)
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
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		resolvedSrc, err := filepath.EvalSymlinks(cleanedSrc)
		if err != nil {
			return err
		}
		resolvedDst, err := resolveAllowMissing(cleanedDst)
		if err != nil {
			return err
		}
		inside, err := pathWithin(resolvedSrc, resolvedDst)
		if err != nil {
			return err
		}
		if inside {
			return fmt.Errorf("cannot copy a directory into itself")
		}
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
		dstDir := filepath.Dir(dst)
		if err := requireDirectory(dstDir); err != nil {
			return err
		}
		if err := os.Symlink(link, dst); err != nil {
			if errors.Is(err, os.ErrExist) {
				return fmt.Errorf("destination already exists: %s", dst)
			}
			return err
		}
		return syncDir(dstDir)
	}
	if !srcInfo.Mode().IsRegular() {
		return fmt.Errorf("unsupported source file type: %s", src)
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	if err := requireDirectory(filepath.Dir(dst)); err != nil {
		return err
	}

	dstDir := filepath.Dir(dst)
	dstFile, err := os.CreateTemp(dstDir, "."+filepath.Base(dst)+".*.tmp")
	if err != nil {
		return err
	}
	dstTmp := dstFile.Name()
	defer func() {
		dstFile.Close()
		_ = os.Remove(dstTmp)
	}()
	if err := dstFile.Chmod(srcInfo.Mode().Perm()); err != nil {
		return err
	}

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}
	if err := dstFile.Sync(); err != nil {
		return err
	}
	if err := dstFile.Close(); err != nil {
		return err
	}
	if err := os.Chtimes(dstTmp, srcInfo.ModTime(), srcInfo.ModTime()); err != nil {
		return err
	}
	if err := renameNoReplace(dstTmp, dst); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("destination already exists: %s", dst)
		}
		return err
	}
	return syncDir(dstDir)
}

func copyDir(src, dst string) error {
	dstParent := filepath.Dir(dst)
	if err := requireDirectory(dstParent); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(dstParent, "."+filepath.Base(dst)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	type directoryMetadata struct {
		path string
		info os.FileInfo
	}
	var directories []directoryMetadata
	err = filepath.Walk(src, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := staging
		if relPath != "." {
			dstPath = filepath.Join(staging, relPath)
		}
		if info.IsDir() {
			if relPath != "." {
				if err := os.Mkdir(dstPath, 0700); err != nil {
					return err
				}
			}
			directories = append(directories, directoryMetadata{path: dstPath, info: info})
			return nil
		}
		return copyFile(path, dstPath)
	})
	if err != nil {
		return err
	}
	for i := len(directories) - 1; i >= 0; i-- {
		directory := directories[i]
		if err := os.Chmod(directory.path, directory.info.Mode().Perm()); err != nil {
			return err
		}
		if err := os.Chtimes(directory.path, directory.info.ModTime(), directory.info.ModTime()); err != nil {
			return err
		}
	}
	if err := renameNoReplace(staging, dst); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("destination already exists: %s", dst)
		}
		return err
	}
	return syncDir(dstParent)
}

func requireDirectory(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("destination parent is not a directory: %s", path)
	}
	return nil
}

func renameNoReplace(src, dst string) error {
	err := unix.Renameat2(unix.AT_FDCWD, src, unix.AT_FDCWD, dst, unix.RENAME_NOREPLACE)
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EINVAL) {
		return fmt.Errorf("filesystem does not support atomic no-replace rename: %w", err)
	}
	return err
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	err = dir.Sync()
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) {
		return nil
	}
	return err
}

func resolveAllowMissing(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := absPath
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func pathWithin(parent, child string) (bool, error) {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false, err
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))), nil
}

func parseFileMode(s string) (os.FileMode, error) {
	if len(s) == 0 || len(s) > 4 {
		return 0, fmt.Errorf("invalid file mode length: %s", s)
	}
	mode, err := strconv.ParseUint(s, 8, 12)
	if err != nil || mode > 0777 {
		return 0, fmt.Errorf("invalid file mode: %s", s)
	}
	return os.FileMode(mode), nil
}
