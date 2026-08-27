package cmd

import (
	"path/filepath"
	"testing"
)

func TestGuardDataDirRefusesOperationsOnTheQueuesOwnState(t *testing.T) {
	dataDir := "/home/user/.file-exploder"

	refused := []string{
		dataDir,                            // the directory itself
		filepath.Join(dataDir, "queue.db"), // the database alone is enough
		"/home/user",                       // an ancestor takes it with them
		"/home",                            //
		"/home/user/.file-exploder/../.file-exploder", // an equivalent spelling
	}
	for _, path := range refused {
		if err := guardDataDir(dataDir, path, ""); err == nil {
			t.Errorf("guardDataDir allowed %q", path)
		}
		if err := guardDataDir(dataDir, "", path); err == nil {
			t.Errorf("guardDataDir allowed %q as a destination", path)
		}
	}

	allowed := []string{
		"/home/user2",
		"/home/user-other/.file-exploder",
		"/srv/data",
		"/home/user/documents",
		"/home/user/.file-exploder-backup", // a sibling that merely shares a prefix
	}
	for _, path := range allowed {
		if err := guardDataDir(dataDir, path, ""); err != nil {
			t.Errorf("guardDataDir refused %q: %v", path, err)
		}
	}

	if err := guardDataDir(dataDir, "", ""); err != nil {
		t.Errorf("guardDataDir refused an empty operation: %v", err)
	}
}
