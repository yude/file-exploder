package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingLogWriterRotatesDuringContinuousRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.log")
	w, err := newRotatingLogWriter(path, 16)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("first-line")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("second-line")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != "second-line" || string(previous) != "first-line" {
		t.Fatalf("current=%q previous=%q", current, previous)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("log mode = %o, want 600", info.Mode().Perm())
	}
}

func TestRotatingLogWriterRotatesOversizedFileAtOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 17)), 0600); err != nil {
		t.Fatal(err)
	}
	w, err := newRotatingLogWriter(path, 16)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatal(err)
	}
}

func TestRotatingLogWriterKeepsWorkingAfterAFailedRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "queue.log")
	w, err := newRotatingLogWriter(path, 16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })

	if _, err := w.Write([]byte("first-line")); err != nil {
		t.Fatal(err)
	}

	// A directory at the rotation target makes os.Rename fail.
	if err := os.Mkdir(path+".1", 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("second-line")); err == nil {
		t.Fatal("expected the failed rotation to be reported")
	}

	// The writer must have reopened the active log rather than wedging.
	if err := os.RemoveAll(path + ".1"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("third-line")); err != nil {
		t.Fatalf("writer did not recover from a failed rotation: %v", err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current), "third-line") {
		t.Fatalf("log = %q, want it to contain third-line", current)
	}
}
