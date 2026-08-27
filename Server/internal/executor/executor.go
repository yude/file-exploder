package executor

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
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

var errNoReplaceUnsupported = errors.New("filesystem does not support atomic no-replace rename")

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
	srcInfo, err := os.Lstat(job.SrcPath)
	if err != nil {
		return err
	}
	// A no-op rename onto the same path succeeds as long as the source exists.
	if filepath.Clean(job.SrcPath) == filepath.Clean(job.DstPath) {
		return nil
	}
	if srcInfo.IsDir() {
		inside, err := destinationInsideSource(job.SrcPath, job.DstPath)
		if err != nil {
			return err
		}
		if inside {
			return fmt.Errorf("cannot move a directory into itself: %s", job.DstPath)
		}
	}

	err = renameNoReplace(job.SrcPath, job.DstPath)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("destination path already exists: %s", job.DstPath)
		}
		if errors.Is(err, syscall.EXDEV) || errors.Is(err, errNoReplaceUnsupported) {
			if copyErr := copyPath(job.SrcPath, job.DstPath); copyErr != nil {
				return fmt.Errorf("rename fallback failed during copy: %w", copyErr)
			}
			currentInfo, statErr := os.Lstat(job.SrcPath)
			if statErr != nil || !os.SameFile(srcInfo, currentInfo) {
				return fmt.Errorf("rename fallback copied the destination but the source changed before deletion")
			}
			if delErr := os.RemoveAll(job.SrcPath); delErr != nil {
				return fmt.Errorf("rename fallback copied the destination but failed to delete the source: %w", delErr)
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
	// Create parents separately and use Mkdir for the leaf. A preflight Lstat
	// followed by MkdirAll has a race: another process can create the leaf in
	// between, after which MkdirAll reports success and this job incorrectly
	// claims it created the directory.
	//
	// Clean first: filepath.Dir("/a/b/") is "/a/b", so a trailing separator
	// would make MkdirAll create the leaf as if it were the parent and Mkdir
	// then fail with "already exists" - against a directory this job had just
	// created itself.
	dst := filepath.Clean(job.DstPath)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if err := os.Mkdir(dst, 0755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("destination path already exists: %s", dst)
		}
		return err
	}
	return nil
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
	mode, err := ParseFileMode(job.Mode)
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
		inside, err := destinationInsideSource(cleanedSrc, cleanedDst)
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
		currentInfo, err := os.Lstat(src)
		if err != nil || !os.SameFile(srcInfo, currentInfo) {
			return fmt.Errorf("source changed while preparing to copy: %s", src)
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

	// #nosec G304 -- opening user-selected paths is the server's core purpose;
	// validation above rejects relative paths and the filesystem root.
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	openedInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(srcInfo, openedInfo) {
		return fmt.Errorf("source changed while preparing to copy: %s", src)
	}
	srcInfo = openedInfo

	if err := requireDirectory(filepath.Dir(dst)); err != nil {
		return err
	}

	dstDir := filepath.Dir(dst)
	dstFile, err := os.CreateTemp(dstDir, stagingPattern(filepath.Base(dst)))
	if err != nil {
		return err
	}
	dstTmp := dstFile.Name()
	defer func() {
		_ = dstFile.Close()
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
	staging, err := os.MkdirTemp(dstParent, stagingPattern(filepath.Base(dst)))
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
	if err := publishStagedDirectory(staging, dst); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("destination already exists: %s", dst)
		}
		return err
	}
	return syncDir(dstParent)
}

// publishStagedDirectory normally publishes the completed tree atomically.
// When the destination filesystem lacks RENAME_NOREPLACE, claim the final name
// with Mkdir and move the already-complete top-level entries into that private
// directory. The original source still exists until this function succeeds, so
// cleaning a failed publication cannot lose user data.
func publishStagedDirectory(staging, dst string) error {
	err := renameNoReplace(staging, dst)
	if err == nil || !errors.Is(err, errNoReplaceUnsupported) {
		return err
	}
	return publishStagedDirectoryWithoutAtomicRename(staging, dst)
}

func publishStagedDirectoryWithoutAtomicRename(staging, dst string) error {
	stagingInfo, err := os.Stat(staging)
	if err != nil {
		return err
	}
	if err := os.Mkdir(dst, 0700); err != nil {
		return err
	}
	claimedInfo, err := os.Lstat(dst)
	if err != nil {
		_ = os.Remove(dst)
		return err
	}
	complete := false
	defer func() {
		if !complete {
			removeClaimedDirectory(dst, claimedInfo)
		}
	}()

	entries, err := os.ReadDir(staging)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.Rename(filepath.Join(staging, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
			return err
		}
	}
	if err := os.Chmod(dst, stagingInfo.Mode().Perm()); err != nil {
		return err
	}
	if err := os.Chtimes(dst, stagingInfo.ModTime(), stagingInfo.ModTime()); err != nil {
		return err
	}
	if err := os.Remove(staging); err != nil {
		return err
	}
	complete = true
	return nil
}

// destinationInsideSource reports whether dst lands inside the src directory
// tree, following symlinks on both sides. This has to be checked before the
// operation runs: rename(2) reports it as EINVAL, which is indistinguishable
// from "this filesystem does not support RENAME_NOREPLACE".
func destinationInsideSource(src, dst string) (bool, error) {
	resolvedSrc, err := filepath.EvalSymlinks(filepath.Clean(src))
	if err != nil {
		return false, err
	}
	resolvedDst, err := resolveAllowMissing(filepath.Clean(dst))
	if err != nil {
		return false, err
	}
	return pathWithin(resolvedSrc, resolvedDst)
}

// stagingPattern builds the temp-name pattern a copy is assembled under.
// os.CreateTemp and os.MkdirTemp append a random suffix to it, so embedding the
// whole destination name overflowed NAME_MAX as soon as that name approached
// the filesystem's 255-byte limit: copying to a long but perfectly legal name
// failed with "file name too long" naming a temp path the user never chose.
// Keep the recognisable part bounded and let the random suffix disambiguate.
func stagingPattern(name string) string {
	const maxNameHint = 64
	hint := name
	if len(hint) > maxNameHint {
		hint = strings.ToValidUTF8(hint[:maxNameHint], "")
	}
	return "." + hint + ".*.tmp"
}

// RemoveOrphanedStaging deletes the half-written staging entry a copy was
// assembling when the daemon died. copyFile and copyDir stage under a temp name
// and rely on a deferred cleanup, which a SIGKILL, an OOM or a crash never runs
// - so the debris was left in the user's own directory, hidden behind a leading
// dot and never reclaimed, holding on to as much space as had been copied.
//
// Only the destination parent of an interrupted job is examined, and only names
// matching the pattern this package generates. Returns the paths it removed.
func RemoveOrphanedStaging(job *queue.Job) ([]string, error) {
	switch job.Type {
	case queue.JobCopy, queue.JobMove, queue.JobRename:
	default:
		// Nothing else stages; rename only does when it falls back to a copy.
		return nil, nil
	}
	if job.DstPath == "" || !filepath.IsAbs(job.DstPath) {
		return nil, nil
	}

	dst := filepath.Clean(job.DstPath)
	dir := filepath.Dir(dst)
	pattern := stagingPattern(filepath.Base(dst))
	star := strings.LastIndex(pattern, "*")
	if star < 0 {
		return nil, nil
	}
	prefix, suffix := pattern[:star], pattern[star+1:]

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var removed []string
	var failures []error
	for _, entry := range entries {
		name := entry.Name()
		if !isStagingName(name, prefix, suffix) {
			continue
		}
		path := filepath.Join(dir, name)
		if err := os.RemoveAll(path); err != nil {
			failures = append(failures, err)
			continue
		}
		removed = append(removed, path)
	}
	return removed, errors.Join(failures...)
}

// isStagingName matches only what os.CreateTemp and os.MkdirTemp produce from
// stagingPattern: the fixed prefix, a run of digits, then the fixed suffix.
func isStagingName(name, prefix, suffix string) bool {
	if len(name) <= len(prefix)+len(suffix) {
		return false
	}
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return false
	}
	digits := name[len(prefix) : len(name)-len(suffix)]
	if digits == "" {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
		if fallbackErr := renameNoReplaceWithPlaceholder(src, dst); fallbackErr != nil {
			if errors.Is(fallbackErr, errNoReplaceUnsupported) {
				return fmt.Errorf("%w: rename %s -> %s", errNoReplaceUnsupported, src, dst)
			}
			return fmt.Errorf("rename %s -> %s without RENAME_NOREPLACE: %w", src, dst, fallbackErr)
		}
		return nil
	}
	return err
}

// renameNoReplaceWithPlaceholder supports regular files on filesystems such as
// CIFS, FUSE and some union mounts that reject renameat2(RENAME_NOREPLACE). A
// preflight Lstat followed by rename is not sufficient: another process can
// create dst in the gap and plain rename would overwrite it. Claim dst
// atomically first, then replace only the empty inode this call created.
//
// There is no fully atomic userspace equivalent to RENAME_NOREPLACE against a
// malicious process with write access to the same parent. SameFile checks make
// cleanup conservative, while O_EXCL preserves no-overwrite behaviour for
// ordinary concurrent file operations and for the daemon's serialized queue.
func renameNoReplaceWithPlaceholder(src, dst string) error {
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return err
	}

	if srcInfo.IsDir() {
		return errNoReplaceUnsupported
	}

	placeholder, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0000)
	if err != nil {
		return err
	}
	placeholderInfo, err := placeholder.Stat()
	closeErr := placeholder.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		removePlaceholder(dst, placeholderInfo)
		return err
	}

	claimed := true
	defer func() {
		if claimed {
			removePlaceholder(dst, placeholderInfo)
		}
	}()

	currentInfo, err := os.Lstat(dst)
	if err != nil || !os.SameFile(placeholderInfo, currentInfo) {
		return fmt.Errorf("destination placeholder changed before rename")
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	claimed = false
	return nil
}

// Remove only the placeholder created by this process. If another actor has
// replaced it, its path must be left alone even while unwinding an error.
func removePlaceholder(path string, placeholderInfo os.FileInfo) {
	if placeholderInfo == nil {
		return
	}
	currentInfo, err := os.Lstat(path)
	if err == nil && os.SameFile(placeholderInfo, currentInfo) {
		_ = os.Remove(path)
	}
}

func removeClaimedDirectory(path string, claimedInfo os.FileInfo) {
	currentInfo, err := os.Lstat(path)
	if err == nil && os.SameFile(claimedInfo, currentInfo) {
		_ = os.RemoveAll(path)
	}
}

func syncDir(path string) error {
	// #nosec G304 -- path is the parent of an already validated operation path.
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

// ParseFileMode parses an octal chmod argument such as "755" or "0644".
func ParseFileMode(s string) (os.FileMode, error) {
	if len(s) == 0 || len(s) > 4 {
		return 0, fmt.Errorf("invalid file mode length: %s", s)
	}
	mode, err := strconv.ParseUint(s, 8, 12)
	if err != nil || mode > 0777 {
		return 0, fmt.Errorf("invalid file mode: %s", s)
	}
	return os.FileMode(mode), nil
}
