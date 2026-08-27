package executor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/yude/file-exploder/server/internal/queue"
)

func TestValidatePaths(t *testing.T) {
	err := validatePaths("/")
	if err == nil {
		t.Error("Expected error for root path /")
	}

	err = validatePaths(".")
	if err == nil {
		t.Error("Expected error for . path")
	}

	err = validatePaths("../outside")
	if err == nil {
		t.Error("Expected error for ../ path")
	}

	err = validatePaths("relative/path")
	if err == nil {
		t.Error("Expected error for relative path")
	}

	err = validatePaths("/valid/path")
	if err != nil {
		t.Errorf("Unexpected error for valid path: %v", err)
	}
}

func TestCopyFileDoesNotOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "source")
	dst := filepath.Join(tmpDir, "destination")
	if err := os.WriteFile(src, []byte("source"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := copyPath(src, dst); err == nil {
		t.Fatal("copy unexpectedly overwrote an existing destination")
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep" {
		t.Fatalf("destination changed: %q", data)
	}
}

func TestRenameNoReplacePlaceholderMovesFileWithoutOverwriting(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "source")
	dst := filepath.Join(tmpDir, "destination")
	if err := os.WriteFile(src, []byte("source"), 0640); err != nil {
		t.Fatal(err)
	}

	if err := renameNoReplaceWithPlaceholder(src, dst); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "source" {
		t.Fatalf("destination = %q, %v", data, err)
	}
	if _, err := os.Lstat(src); !os.IsNotExist(err) {
		t.Fatalf("source survived rename: %v", err)
	}

	secondSrc := filepath.Join(tmpDir, "second-source")
	if err := os.WriteFile(secondSrc, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := renameNoReplaceWithPlaceholder(secondSrc, dst); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing destination error = %v, want ErrExist", err)
	}
	data, err = os.ReadFile(dst)
	if err != nil || string(data) != "source" {
		t.Fatalf("existing destination changed to %q, %v", data, err)
	}
}

func TestPublishStagedDirectoryWithoutAtomicRename(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "staging")
	dst := filepath.Join(tmpDir, "destination")
	if err := os.Mkdir(src, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "file"), []byte("content"), 0640); err != nil {
		t.Fatal(err)
	}

	if err := publishStagedDirectoryWithoutAtomicRename(src, dst); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dst, "file"))
	if err != nil || string(data) != "content" {
		t.Fatalf("copied directory content = %q, %v", data, err)
	}

	secondStaging := filepath.Join(tmpDir, "second-staging")
	if err := os.Mkdir(secondStaging, 0700); err != nil {
		t.Fatal(err)
	}
	if err := publishStagedDirectoryWithoutAtomicRename(secondStaging, dst); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing destination error = %v, want ErrExist", err)
	}
	data, err = os.ReadFile(filepath.Join(dst, "file"))
	if err != nil || string(data) != "content" {
		t.Fatalf("existing directory changed: %q, %v", data, err)
	}
}

func TestCopyDirectoryIsStagedAndPreservesSymlink(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "source")
	dst := filepath.Join(tmpDir, "destination")
	if err := os.Mkdir(src, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "file"), []byte("content"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("file", filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}

	if err := copyPath(src, dst); err != nil {
		t.Fatal(err)
	}
	link, err := os.Readlink(filepath.Join(dst, "link"))
	if err != nil {
		t.Fatal(err)
	}
	if link != "file" {
		t.Fatalf("unexpected symlink target: %q", link)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0750 {
		t.Fatalf("directory mode = %o, want 750", info.Mode().Perm())
	}
}

func TestCopyDirectoryRejectsSymlinkedDescendant(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "source")
	if err := os.Mkdir(src, 0755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(tmpDir, "alias")
	if err := os.Symlink(src, alias); err != nil {
		t.Fatal(err)
	}

	if err := copyPath(src, filepath.Join(alias, "nested")); err == nil {
		t.Fatal("copy into a symlinked descendant unexpectedly succeeded")
	}
}

func TestCopyDoesNotCreateMissingParents(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "source")
	if err := os.WriteFile(src, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmpDir, "missing", "destination")
	if err := copyPath(src, dst); err == nil {
		t.Fatal("copy unexpectedly created missing destination parents")
	}
	if _, err := os.Stat(filepath.Dir(dst)); !os.IsNotExist(err) {
		t.Fatalf("destination parent was created: %v", err)
	}
}

func TestParseFileMode(t *testing.T) {
	for _, valid := range []string{"0", "600", "0755"} {
		if _, err := ParseFileMode(valid); err != nil {
			t.Errorf("ParseFileMode(%q): %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "888", "755junk", "4755"} {
		if _, err := ParseFileMode(invalid); err == nil {
			t.Errorf("ParseFileMode(%q) unexpectedly succeeded", invalid)
		}
	}
}

func TestExecuteMkdirAndDelete(t *testing.T) {
	e := NewExecutor(nil) // Queue interface not strictly needed for these operations

	tmpDir := t.TempDir()

	jobMkdir := &queue.Job{Type: queue.JobMkdir, DstPath: filepath.Join(tmpDir, "new_dir")}
	err := e.Execute(jobMkdir)
	if err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, "new_dir")); os.IsNotExist(err) {
		t.Fatal("Directory was not created")
	}

	jobDel := &queue.Job{Type: queue.JobDelete, SrcPath: filepath.Join(tmpDir, "new_dir")}
	err = e.Execute(jobDel)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, "new_dir")); !os.IsNotExist(err) {
		t.Fatal("Directory was not deleted")
	}
}

func TestExecuteChmodRejectsSymlink(t *testing.T) {
	e := NewExecutor(nil)
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "target")
	link := filepath.Join(tmpDir, "link")
	if err := os.WriteFile(target, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	err := e.Execute(&queue.Job{Type: queue.JobChmod, DstPath: link, Mode: "0777"})
	if err == nil {
		t.Fatal("chmod unexpectedly followed a symbolic link")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("target mode changed to %o", info.Mode().Perm())
	}
}

func TestExecuteRenameRejectsDirectoryIntoItself(t *testing.T) {
	e := NewExecutor(nil)
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "source")
	if err := os.Mkdir(src, 0755); err != nil {
		t.Fatal(err)
	}

	err := e.Execute(&queue.Job{
		Type:    queue.JobMove,
		SrcPath: src,
		DstPath: filepath.Join(src, "nested"),
	})
	if err == nil {
		t.Fatal("moving a directory into itself unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "into itself") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(src); statErr != nil {
		t.Fatalf("source directory disturbed: %v", statErr)
	}
}

func TestExecuteRenameOnSamePathIsANoOp(t *testing.T) {
	e := NewExecutor(nil)
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "source")
	if err := os.WriteFile(src, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := e.Execute(&queue.Job{Type: queue.JobRename, SrcPath: src, DstPath: src + "/../source"}); err != nil {
		t.Fatalf("no-op rename failed: %v", err)
	}
	if err := e.Execute(&queue.Job{
		Type:    queue.JobRename,
		SrcPath: filepath.Join(tmpDir, "missing"),
		DstPath: filepath.Join(tmpDir, "missing"),
	}); err == nil {
		t.Fatal("no-op rename of a missing source unexpectedly succeeded")
	}
}

func TestExecuteMkdirRefusesAnExistingPath(t *testing.T) {
	e := NewExecutor(nil)
	tmpDir := t.TempDir()

	existingDir := filepath.Join(tmpDir, "already-there")
	if err := os.Mkdir(existingDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := e.Execute(&queue.Job{Type: queue.JobMkdir, DstPath: existingDir}); err == nil {
		t.Fatal("mkdir over an existing directory unexpectedly succeeded")
	}

	existingFile := filepath.Join(tmpDir, "file")
	if err := os.WriteFile(existingFile, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.Execute(&queue.Job{Type: queue.JobMkdir, DstPath: existingFile}); err == nil {
		t.Fatal("mkdir over an existing file unexpectedly succeeded")
	}
	if data, err := os.ReadFile(existingFile); err != nil || string(data) != "content" {
		t.Fatalf("existing file disturbed: %q, %v", data, err)
	}

	// Missing parents are still created.
	nested := filepath.Join(tmpDir, "a", "b", "c")
	if err := e.Execute(&queue.Job{Type: queue.JobMkdir, DstPath: nested}); err != nil {
		t.Fatalf("nested mkdir failed: %v", err)
	}
	if info, err := os.Stat(nested); err != nil || !info.IsDir() {
		t.Fatalf("nested directory not created: %v", err)
	}
}

func TestExecuteMkdirAcceptsATrailingSeparator(t *testing.T) {
	e := NewExecutor(nil)
	tmpDir := t.TempDir()

	// filepath.Dir("/a/b/") is "/a/b", so an uncleaned path made MkdirAll
	// create the leaf and Mkdir then reject the directory it had just made.
	target := filepath.Join(tmpDir, "with-slash") + string(filepath.Separator)
	if err := e.Execute(&queue.Job{Type: queue.JobMkdir, DstPath: target}); err != nil {
		t.Fatalf("mkdir with a trailing separator failed: %v", err)
	}
	info, err := os.Stat(filepath.Join(tmpDir, "with-slash"))
	if err != nil || !info.IsDir() {
		t.Fatalf("directory not created: %v", err)
	}

	// Repeating it must still be refused.
	if err := e.Execute(&queue.Job{Type: queue.JobMkdir, DstPath: target}); err == nil {
		t.Fatal("second mkdir over the same directory unexpectedly succeeded")
	}
}

func TestCopyToNamesNearTheFilesystemLimit(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "source")
	if err := os.WriteFile(src, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(tmpDir, "sourcedir")
	if err := os.Mkdir(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "inner"), []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}

	// 255 bytes is the usual NAME_MAX; the staging name used to add the whole
	// destination name on top of it and blow past the limit.
	longName := strings.Repeat("f", 255)
	if err := copyPath(src, filepath.Join(tmpDir, longName)); err != nil {
		t.Fatalf("copying a file to a 255-character name failed: %v", err)
	}
	if err := copyPath(srcDir, filepath.Join(tmpDir, strings.Repeat("d", 255))); err != nil {
		t.Fatalf("copying a directory to a 255-character name failed: %v", err)
	}
}

func TestStagingPatternStaysShortAndValid(t *testing.T) {
	pattern := stagingPattern(strings.Repeat("あ", 200))
	if len(pattern) > 96 {
		t.Fatalf("pattern is %d bytes: %q", len(pattern), pattern)
	}
	if !utf8.ValidString(pattern) {
		t.Fatalf("pattern truncated mid-rune: %q", pattern)
	}
}

func TestRemoveOrphanedStagingReclaimsInterruptedCopies(t *testing.T) {
	tmpDir := t.TempDir()
	dst := filepath.Join(tmpDir, "destination")

	// What a SIGKILL during copyDir and copyFile leaves behind.
	stagingDir := filepath.Join(tmpDir, ".destination.123456.tmp")
	if err := os.Mkdir(stagingDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "partial"), []byte("half"), 0600); err != nil {
		t.Fatal(err)
	}
	stagingFile := filepath.Join(tmpDir, ".destination.987.tmp")
	if err := os.WriteFile(stagingFile, []byte("half"), 0600); err != nil {
		t.Fatal(err)
	}

	// Everything else must survive, including names that only resemble the
	// pattern this package generates.
	keep := []string{
		"destination",                 // a finished copy from another run
		".destination.tmp",            // no random component
		".destination.abc.tmp",        // not a number
		".destination.123456.tmp.bak", // wrong suffix
		".other.123456.tmp",           // another destination's staging
		".hidden",
	}
	for _, name := range keep {
		if err := os.WriteFile(filepath.Join(tmpDir, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := RemoveOrphanedStaging(&queue.Job{Type: queue.JobCopy, DstPath: dst})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 2 {
		t.Fatalf("removed %v, want both staging entries", removed)
	}
	for _, path := range []string{stagingDir, stagingFile} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s survived: %v", path, err)
		}
	}
	for _, name := range keep {
		if _, err := os.Stat(filepath.Join(tmpDir, name)); err != nil {
			t.Errorf("%s was removed: %v", name, err)
		}
	}
}

func TestRemoveOrphanedStagingIgnoresJobsThatNeverStage(t *testing.T) {
	tmpDir := t.TempDir()
	staging := filepath.Join(tmpDir, ".destination.1.tmp")
	if err := os.WriteFile(staging, nil, 0600); err != nil {
		t.Fatal(err)
	}

	for _, jobType := range []queue.JobType{queue.JobMkdir, queue.JobChmod, queue.JobDelete} {
		removed, err := RemoveOrphanedStaging(&queue.Job{Type: jobType, DstPath: filepath.Join(tmpDir, "destination")})
		if err != nil || len(removed) != 0 {
			t.Errorf("%s: removed %v, %v", jobType, removed, err)
		}
	}
	if _, err := os.Stat(staging); err != nil {
		t.Fatalf("staging removed for a job type that never stages: %v", err)
	}
}

func TestOperationsAcceptATrailingSeparatorOnPaths(t *testing.T) {
	e := NewExecutor(nil)
	sep := string(filepath.Separator)

	t.Run("copy file", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "source")
		if err := os.WriteFile(src, []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := e.Execute(&queue.Job{Type: queue.JobCopy, SrcPath: src, DstPath: filepath.Join(dir, "copy") + sep}); err != nil {
			t.Fatalf("copy failed: %v", err)
		}
		if data, err := os.ReadFile(filepath.Join(dir, "copy")); err != nil || string(data) != "content" {
			t.Fatalf("copy = %q, %v", data, err)
		}
	})

	t.Run("copy directory", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "source")
		if err := os.Mkdir(src, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "inner"), []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := e.Execute(&queue.Job{Type: queue.JobCopy, SrcPath: src + sep, DstPath: filepath.Join(dir, "copy") + sep}); err != nil {
			t.Fatalf("copy failed: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "copy", "inner")); err != nil {
			t.Fatalf("directory copy incomplete: %v", err)
		}
	})

	t.Run("move file", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "source")
		if err := os.WriteFile(src, []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := e.Execute(&queue.Job{Type: queue.JobMove, SrcPath: src, DstPath: filepath.Join(dir, "moved") + sep}); err != nil {
			t.Fatalf("move failed: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "moved")); err != nil {
			t.Fatalf("move did not land: %v", err)
		}
	})

	t.Run("chmod file", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "target")
		if err := os.WriteFile(target, []byte("content"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := e.Execute(&queue.Job{Type: queue.JobChmod, DstPath: target + sep, Mode: "0644"}); err != nil {
			t.Fatalf("chmod failed: %v", err)
		}
		info, err := os.Stat(target)
		if err != nil || info.Mode().Perm() != 0644 {
			t.Fatalf("mode = %v, %v", info.Mode().Perm(), err)
		}
	})
}

func TestChmodStillRefusesASymlinkSpelledWithATrailingSeparator(t *testing.T) {
	e := NewExecutor(nil)
	dir := t.TempDir()
	target := filepath.Join(dir, "realdir")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	// A trailing separator forces the kernel to resolve the path as a directory,
	// which used to make Lstat report the target and let the job widen it.
	if err := e.Execute(&queue.Job{Type: queue.JobChmod, DstPath: link + string(filepath.Separator), Mode: "0777"}); err == nil {
		t.Fatal("chmod through a trailing-separator symlink unexpectedly succeeded")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("symlink target widened to %o", info.Mode().Perm())
	}
}

func TestCopyPreservesSetuidSetgidAndSticky(t *testing.T) {
	dir := t.TempDir()

	src := filepath.Join(dir, "binary")
	if err := os.WriteFile(src, []byte("content"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(src, 0755|os.ModeSetuid|os.ModeSetgid); err != nil {
		t.Fatal(err)
	}
	if err := copyPath(src, filepath.Join(dir, "binary-copy")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(dir, "binary-copy"))
	if err != nil {
		t.Fatal(err)
	}
	if got := copyableMode(info.Mode()); got != 0755|os.ModeSetuid|os.ModeSetgid {
		t.Errorf("file copy mode = %v, want setuid+setgid 0755", got)
	}

	srcDir := filepath.Join(dir, "tree")
	if err := os.Mkdir(srcDir, 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "inner"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(srcDir, 0775|os.ModeSetgid|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	if err := copyPath(srcDir, filepath.Join(dir, "tree-copy")); err != nil {
		t.Fatal(err)
	}
	dirInfo, err := os.Lstat(filepath.Join(dir, "tree-copy"))
	if err != nil {
		t.Fatal(err)
	}
	if got := copyableMode(dirInfo.Mode()); got != 0775|os.ModeSetgid|os.ModeSticky {
		t.Errorf("directory copy mode = %v, want setgid+sticky 0775", got)
	}
}

// The RENAME_NOREPLACE fallbacks cannot be reached through a normal copy here:
// tmpfs and ext4 both support the flag. Drive them directly.
func TestStagedDirectoryPublishKeepsSpecialBits(t *testing.T) {
	dir := t.TempDir()
	staging := filepath.Join(dir, ".staging")
	if err := os.Mkdir(staging, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "inner"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// What copyDir hands over: staging already carries the source's mode.
	wanted := os.FileMode(0775) | os.ModeSetgid | os.ModeSticky
	if err := os.Chmod(staging, wanted); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "published")
	if err := publishStagedDirectoryWithoutAtomicRename(staging, dst); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if got := copyableMode(info.Mode()); got != wanted {
		t.Errorf("published mode = %v, want %v", got, wanted)
	}
	if _, err := os.Stat(filepath.Join(dst, "inner")); err != nil {
		t.Errorf("contents not published: %v", err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Errorf("staging survived: %v", err)
	}
}

func TestPlaceholderRenameKeepsModeAndRefusesAnOccupiedDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	if err := os.WriteFile(src, []byte("content"), 0640); err != nil {
		t.Fatal(err)
	}
	wanted := os.FileMode(0640) | os.ModeSetuid
	if err := os.Chmod(src, wanted); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "moved")

	if err := renameNoReplaceWithPlaceholder(src, dst); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if got := copyableMode(info.Mode()); got != wanted {
		t.Errorf("moved mode = %v, want %v", got, wanted)
	}

	// The whole point of the fallback is that it still refuses to replace.
	if err := os.WriteFile(src, []byte("again"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := renameNoReplaceWithPlaceholder(src, dst); err == nil {
		t.Fatal("placeholder rename overwrote an existing destination")
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "content" {
		t.Fatalf("destination changed: %q, %v", data, err)
	}
}
