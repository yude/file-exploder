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
	"time"

	"github.com/yude/file-exploder/server/internal/queue"
	"golang.org/x/sys/unix"
)

// symlinkResolveTimeout bounds a single symlink resolution the same way
// cmd/list.go's constant of the same name bounds one there - this package
// cannot import cmd (cmd already imports executor), so it keeps its own copy
// rather than share the value across a package boundary that doesn't exist.
const symlinkResolveTimeout = 2 * time.Second

type Executor struct {
	q queue.Queue
}

var errNoReplaceUnsupported = errors.New("filesystem does not support atomic no-replace rename")

func NewExecutor(q queue.Queue) *Executor {
	return &Executor{q: q}
}

func (e *Executor) Execute(job *queue.Job) error {
	// Every operation works on cleaned paths. filepath.Dir("/a/b/") is "/a/b",
	// so a trailing separator made every parent-directory computation point at
	// the destination itself: copy and move failed with "no such file or
	// directory" naming the very path they had been asked to create. Worse, a
	// trailing separator forces the kernel to resolve the path as a directory,
	// so os.Lstat reported the *target* of a symlink - which walked straight
	// past executeChmod's refusal to chmod a link and changed the target's mode
	// instead.
	//
	// Normalise on a copy so the queue's own record of the job is left alone.
	normalized := *job
	if normalized.SrcPath != "" {
		normalized.SrcPath = filepath.Clean(normalized.SrcPath)
	}
	if normalized.DstPath != "" {
		normalized.DstPath = filepath.Clean(normalized.DstPath)
	}
	job = &normalized

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
			dstInfo, statErr := os.Lstat(job.DstPath)
			if statErr != nil || !srcInfo.IsDir() || !dstInfo.IsDir() {
				return fmt.Errorf("destination path already exists: %s", job.DstPath)
			}
			// Explorer-style folder merge: copy into the existing directory
			// first, and remove the source only after the whole merge succeeds.
			if copyErr := copyPath(job.SrcPath, job.DstPath); copyErr != nil {
				return fmt.Errorf("directory merge failed during copy: %w", copyErr)
			}
			currentInfo, sourceErr := os.Lstat(job.SrcPath)
			if sourceErr != nil || !os.SameFile(srcInfo, currentInfo) {
				return fmt.Errorf("directory merge copied the destination but the source changed before deletion")
			}
			if delErr := os.RemoveAll(job.SrcPath); delErr != nil {
				return fmt.Errorf("directory merge copied the destination but failed to delete the source: %w", delErr)
			}
			return nil
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
	// claims it created the directory. Execute has already cleaned the path.
	dst := job.DstPath
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

	// A separate Lstat-then-Chmod has a TOCTOU gap: Chmod always follows the
	// final path component (there is no portable lchmod), so a symlink
	// swapped in at this name between the Lstat check and the Chmod call
	// would be chmod'd through to whatever it points to instead of being
	// refused. O_NOFOLLOW makes the open itself fail with ELOOP when the
	// final component is a symlink, and Fchmod then acts on the exact inode
	// that was opened - so the check and the mode change are atomic with
	// respect to that path.
	fd, err := unix.Open(job.DstPath, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return fmt.Errorf("refusing to chmod a symbolic link: %s", job.DstPath)
		}
		return &os.PathError{Op: "open", Path: job.DstPath, Err: err}
	}
	defer unix.Close(fd)
	return unix.Fchmod(fd, uint32(mode))
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
	dstInfo, dstErr := os.Lstat(cleanedDst)
	if dstErr != nil && !errors.Is(dstErr, fs.ErrNotExist) {
		return dstErr
	}
	if dstErr == nil && (!info.IsDir() || !dstInfo.IsDir()) {
		return fmt.Errorf("destination already exists: %s", cleanedDst)
	}
	if info.IsDir() {
		inside, err := destinationInsideSource(cleanedSrc, cleanedDst)
		if err != nil {
			return err
		}
		if inside {
			return fmt.Errorf("cannot copy a directory into itself")
		}
		if dstErr == nil {
			if err := validateDirectoryMerge(src, dst); err != nil {
				return err
			}
		}
		return copyDir(src, dst)
	}
	return copyFile(src, dst)
}

// copyableMode is the part of a mode a copy reproduces: the permission bits
// plus setuid, setgid and sticky. Mode().Perm() alone masks to 0777 and silently
// dropped the three special bits, so a setgid directory stopped granting its
// group to new files and a setuid binary came back unprivileged.
func copyableMode(mode os.FileMode) os.FileMode {
	return mode & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky)
}

// copyFile copies a single file or symlink and fsyncs its destination
// directory so the new entry survives a crash. copyDir instead calls
// copyFileTo directly with syncParent false: it fsyncs each staging directory
// itself exactly once after every entry has been written into it (see the
// directories loop in copyDir), rather than paying one fsync per file for a
// directory entry that copyDir is about to sync anyway.
func copyFile(src, dst string) error {
	return copyFileTo(src, dst, true)
}

func copyFileTo(src, dst string, syncParent bool) error {
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
		if !syncParent {
			return nil
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
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}
	// After the copy, not before: writing to a file can clear setuid and setgid.
	if err := dstFile.Chmod(copyableMode(srcInfo.Mode())); err != nil {
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
	if !syncParent {
		return nil
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
	// Suppressed once mergeStagedDirectory fails partway through: at that
	// point staging still holds whatever didn't make it into dst before the
	// error, and deleting it here would discard that with no record of what
	// was lost and no way to finish the operation. Every other failure path
	// leaves dst untouched, so staging is just a redundant copy of src and
	// safe to discard as before.
	removeStaging := true
	defer func() {
		if removeStaging {
			os.RemoveAll(staging)
		}
	}()

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
		return copyFileTo(path, dstPath, false)
	})
	if err != nil {
		return err
	}
	for i := len(directories) - 1; i >= 0; i-- {
		directory := directories[i]
		if err := os.Chmod(directory.path, copyableMode(directory.info.Mode())); err != nil {
			return err
		}
		if err := os.Chtimes(directory.path, directory.info.ModTime(), directory.info.ModTime()); err != nil {
			return err
		}
		// One fsync per directory instead of one per file: every entry that
		// belongs in it (files and, since this loop runs after the whole walk
		// completes, subdirectories too) has already been written by this
		// point, so a single sync here durably covers all of them.
		if err := syncDir(directory.path); err != nil {
			return err
		}
	}
	if dstInfo, dstErr := os.Lstat(dst); dstErr == nil && dstInfo.IsDir() {
		// Recheck after staging: the destination can change during a long copy.
		if err := validateDirectoryMerge(staging, dst); err != nil {
			return err
		}
		if err := mergeStagedDirectory(staging, dst); err != nil {
			removeStaging = false
			rescue, rescueErr := rescueUnmergedStaging(staging)
			if rescueErr != nil {
				return fmt.Errorf("directory merge failed partway through (%w), and the unmerged remainder at %s could not be moved aside for recovery: %w",
					err, staging, rescueErr)
			}
			return fmt.Errorf("directory merge failed partway through, leaving copied-but-not-yet-merged entries at %s for manual recovery: %w",
				rescue, err)
		}
	} else if dstErr != nil && !errors.Is(dstErr, fs.ErrNotExist) {
		return dstErr
	} else if dstErr == nil {
		return fmt.Errorf("destination already exists: %s", dst)
	} else if err := publishStagedDirectory(staging, dst); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("destination already exists: %s", dst)
		}
		return err
	}
	return syncDir(dstParent)
}

// validateDirectoryMerge performs a read-only preflight so ordinary file
// conflicts are reported before a potentially long staging copy starts.
// Directories may overlap recursively; every other occupied name is left for
// the user to resolve rather than being overwritten or silently skipped.
func validateDirectoryMerge(src, dst string) error {
	return filepath.Walk(src, func(srcPath string, srcInfo os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relPath, err := filepath.Rel(src, srcPath)
		if err != nil {
			return err
		}
		dstPath := dst
		if relPath != "." {
			dstPath = filepath.Join(dst, relPath)
		}
		dstInfo, err := os.Lstat(dstPath)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if srcInfo.IsDir() && dstInfo.IsDir() {
			return nil
		}
		return fmt.Errorf("destination entry already exists: %s", dstPath)
	})
}

// mergeStagedDirectory publishes only missing entries into an existing
// directory. The caller has already preflighted conflicts, but every rename
// still uses no-replace semantics to protect against concurrent changes.
func mergeStagedDirectory(staging, dst string) error {
	entries, err := os.ReadDir(staging)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(staging, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		srcInfo, err := os.Lstat(srcPath)
		if err != nil {
			return err
		}
		dstInfo, dstErr := os.Lstat(dstPath)
		switch {
		case errors.Is(dstErr, fs.ErrNotExist):
			if srcInfo.IsDir() {
				err = publishStagedDirectory(srcPath, dstPath)
			} else {
				err = renameNoReplace(srcPath, dstPath)
			}
			if err != nil {
				if errors.Is(err, os.ErrExist) {
					return fmt.Errorf("destination entry appeared during merge: %s", dstPath)
				}
				return err
			}
		case dstErr != nil:
			return dstErr
		case srcInfo.IsDir() && dstInfo.IsDir():
			if err := mergeStagedDirectory(srcPath, dstPath); err != nil {
				return err
			}
		default:
			return fmt.Errorf("destination entry appeared during merge: %s", dstPath)
		}
	}
	if err := os.Remove(staging); err != nil {
		return err
	}
	return syncDir(dst)
}

// rescueUnmergedStaging moves a staging directory mergeStagedDirectory only
// partially drained out of the hidden, dot-prefixed namespace
// RemoveOrphanedStaging sweeps on daemon startup. Left under its original
// name, an unrelated later job that fails and gets interrupted at the same
// destination could have its cleanup silently delete these still-unmerged
// entries before anyone gets to recover them; stripping the leading dot
// (MkdirTemp's random suffix already makes the name unique) takes it out of
// that sweep for good.
func rescueUnmergedStaging(staging string) (string, error) {
	dir := filepath.Dir(staging)
	base := strings.TrimPrefix(filepath.Base(staging), ".")
	rescued := filepath.Join(dir, "file-exploder-unmerged-"+base)
	if err := os.Rename(staging, rescued); err != nil {
		return "", err
	}
	return rescued, nil
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
	// Set once any entry has been moved out of staging into dst. Until then,
	// a failure means dst is still empty and safe to discard; once it isn't,
	// discarding it would destroy a genuine partial result.
	migrated := false
	defer func() {
		if !complete && !migrated {
			removeClaimedDirectory(dst, claimedInfo)
		}
	}()

	entries, err := os.ReadDir(staging)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.Rename(filepath.Join(staging, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
			if !migrated {
				return err
			}
			// Some entries already left staging for dst before this one
			// failed: dst now holds a genuine partial result and must not be
			// discarded (the defer above leaves it alone once migrated is
			// true), and staging still holds whatever never made it across.
			// Rescue that remainder the same way a partially merged
			// directory already is, instead of losing one half or the other.
			if rescued, rescueErr := rescueUnmergedStaging(staging); rescueErr == nil {
				return fmt.Errorf("publishing %s failed partway through: %w (already-published entries are at %s; the rest was rescued to %s)",
					dst, err, dst, rescued)
			}
			return fmt.Errorf("publishing %s failed partway through (%w), and the unpublished remainder at %s could not be rescued", dst, err, staging)
		}
		migrated = true
	}
	// copyableMode, not Perm(): copyDir has already put the source's setuid,
	// setgid and sticky bits on the staging directory, and masking to 0777 here
	// would drop them again on exactly the filesystems that take this path.
	if err := os.Chmod(dst, copyableMode(stagingInfo.Mode())); err != nil {
		return err
	}
	if err := os.Chtimes(dst, stagingInfo.ModTime(), stagingInfo.ModTime()); err != nil {
		return err
	}
	// Without this, a crash right after this function reports success could
	// lose the entries it just moved into dst: only dstParent gets synced
	// back in copyDir, which records that dst's own directory entry exists,
	// not that dst's newly-moved-in contents are durable.
	if err := syncDir(dst); err != nil {
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
	resolvedSrc, err := resolveWithTimeout(filepath.EvalSymlinks, filepath.Clean(src), symlinkResolveTimeout)
	if err != nil {
		return false, err
	}
	resolvedDst, err := resolveWithTimeout(ResolveAllowMissing, filepath.Clean(dst), symlinkResolveTimeout)
	if err != nil {
		return false, err
	}
	return pathWithin(resolvedSrc, resolvedDst)
}

// resolveWithTimeout bounds a symlink-resolving call (filepath.EvalSymlinks or
// ResolveAllowMissing) the same way cmd/add.go's guardDataDir bounds its own -
// a symlink on the way to src or dst can point into a stale "hard" NFS/SMB/
// FUSE mount, and both underlying calls block in uninterruptible sleep on such
// a mount with no way for Go to cancel it. destinationInsideSource runs on
// every directory move or copy the daemon executes (see its two call sites
// above), so without this bound one such job hangs until the daemon's own
// per-job timeout gives up - default 24h, versus every other symlink
// resolution in this codebase (list.go, add.go's guardDataDir) being capped
// at symlinkResolveTimeout.
func resolveWithTimeout(resolve func(string) (string, error), path string, timeout time.Duration) (string, error) {
	type result struct {
		path string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resolved, err := resolve(path)
		done <- result{resolved, err}
	}()
	select {
	case r := <-done:
		return r.path, r.err
	case <-time.After(timeout):
		return "", fmt.Errorf("timed out resolving symlinks in %s after %s", path, timeout)
	}
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
		if placeholderInfo == nil {
			// Stat on the fd we just created failed, so there is nothing to
			// compare dst against before removing it - but O_EXCL means this
			// call is the only thing that could just have created it.
			// removePlaceholder's SameFile guard can never fire with a nil
			// placeholderInfo, which left this mode-0000 file at dst
			// forever; remove it directly instead.
			_ = os.Remove(dst)
			return err
		}
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

// ResolveAllowMissing resolves symlinks in path, tolerating components that do
// not exist yet: the deepest existing ancestor is resolved and the missing tail
// re-appended.
func ResolveAllowMissing(path string) (string, error) {
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
