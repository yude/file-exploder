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
