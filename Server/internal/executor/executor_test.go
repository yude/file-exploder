package executor

import (
	"github.com/yude/file-exploder/server/internal/queue"
	"os"
	"path/filepath"
	"testing"
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

	err = validatePaths("/valid/path")
	if err != nil {
		t.Errorf("Unexpected error for valid path: %v", err)
	}
}

func TestExecuteMkdirAndDelete(t *testing.T) {
	e := NewExecutor(nil) // Queue interface not strictly needed for these operations

	tmpDir := filepath.Join(os.TempDir(), "file-exploder-test")
	defer os.RemoveAll(tmpDir)

	jobMkdir := &queue.Job{Type: queue.JobMkdir, DstPath: tmpDir}
	err := e.Execute(jobMkdir)
	if err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}

	if _, err := os.Stat(tmpDir); os.IsNotExist(err) {
		t.Fatal("Directory was not created")
	}

	jobDel := &queue.Job{Type: queue.JobDelete, SrcPath: tmpDir}
	err = e.Execute(jobDel)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if _, err := os.Stat(tmpDir); !os.IsNotExist(err) {
		t.Fatal("Directory was not deleted")
	}
}
