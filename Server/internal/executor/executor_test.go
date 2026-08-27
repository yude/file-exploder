package executor

import (
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
