package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewFileInfoDescribesSymlinks(t *testing.T) {
	tmpDir := t.TempDir()
	dir := filepath.Join(tmpDir, "dir")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	linkToDir := filepath.Join(tmpDir, "link-to-dir")
	if err := os.Symlink(dir, linkToDir); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(tmpDir, "broken")
	if err := os.Symlink(filepath.Join(tmpDir, "missing"), broken); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		path      string
		isDir     bool
		isSymlink bool
	}{
		{path: dir, isDir: true, isSymlink: false},
		{path: linkToDir, isDir: true, isSymlink: true},
		{path: broken, isDir: false, isSymlink: true},
	} {
		info, err := os.Lstat(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		got := newFileInfo(tc.path, info.Name(), info)
		if got.IsDirectory != tc.isDir || got.IsSymlink != tc.isSymlink {
			t.Errorf("%s: isDirectory=%v isSymlink=%v, want %v/%v",
				tc.path, got.IsDirectory, got.IsSymlink, tc.isDir, tc.isSymlink)
		}
	}
}

func TestListDirectoryIncludesBrokenSymlinks(t *testing.T) {
	tmpDir := t.TempDir()
	dangling := filepath.Join(tmpDir, "dangling")
	if err := os.Symlink(filepath.Join(tmpDir, "missing"), dangling); err != nil {
		t.Fatal(err)
	}
	entries, err := listDirectory(tmpDir)
	if err != nil {
		t.Fatalf("listing a directory with a dangling symlink failed: %v", err)
	}
	if len(entries) != 1 || !entries[0].IsSymlink {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestListDirectorySkipsNamesThatAreNotUTF8(t *testing.T) {
	tmpDir := t.TempDir()
	invalidName := string([]byte{0xff, 0xfe})
	if err := os.WriteFile(filepath.Join(tmpDir, invalidName), nil, 0600); err != nil {
		t.Fatal(err)
	}
	entries, err := listDirectory(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid UTF-8 entry was exposed as %#v", entries)
	}
}

func TestListDirectoryRejectsATargetThatIsNotUTF8(t *testing.T) {
	tmpDir := t.TempDir()
	// The entry is perfectly representable; the directory holding it is not,
	// and every path in the response is built by joining onto it.
	target := filepath.Join(tmpDir, string([]byte{0xff, 0xfe}))
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "normal.txt"), nil, 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := listDirectory(target); err == nil {
		t.Fatal("listing an unrepresentable directory unexpectedly succeeded")
	}
}

func TestStatWithTimeoutReturnsAPromptResult(t *testing.T) {
	tmpDir := t.TempDir()
	info, ok := statWithTimeoutUsing(os.Stat, tmpDir, time.Second)
	if !ok || !info.IsDir() {
		t.Fatalf("statWithTimeoutUsing(%q) = %v, %v", tmpDir, info, ok)
	}

	if _, ok := statWithTimeoutUsing(os.Stat, filepath.Join(tmpDir, "missing"), time.Second); ok {
		t.Fatal("statWithTimeoutUsing reported success for a path that does not exist")
	}
}

func TestStatWithTimeoutGivesUpOnAHungStat(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	hungStat := func(string) (os.FileInfo, error) {
		<-release
		return nil, os.ErrDeadlineExceeded
	}

	start := time.Now()
	_, ok := statWithTimeoutUsing(hungStat, "/anything", 20*time.Millisecond)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("statWithTimeoutUsing waited %s instead of giving up at its timeout", elapsed)
	}
	if ok {
		t.Fatal("statWithTimeoutUsing reported success for a stat that never returned")
	}
}
