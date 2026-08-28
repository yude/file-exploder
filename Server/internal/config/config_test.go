package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfigUsesTheDefaultJobTimeoutWithoutTheEnvVar(t *testing.T) {
	t.Setenv("FILE_EXPLODER_JOB_TIMEOUT", "")
	cfg := DefaultConfig()
	if cfg.JobTimeout != DefaultJobTimeout {
		t.Fatalf("JobTimeout = %v, want the default %v", cfg.JobTimeout, DefaultJobTimeout)
	}
}

func TestDefaultConfigHonoursTheJobTimeoutEnvVar(t *testing.T) {
	t.Setenv("FILE_EXPLODER_JOB_TIMEOUT", "45m")
	cfg := DefaultConfig()
	if cfg.JobTimeout != 45*time.Minute {
		t.Fatalf("JobTimeout = %v, want 45m", cfg.JobTimeout)
	}
}

func TestDefaultConfigIgnoresAnInvalidJobTimeout(t *testing.T) {
	for _, value := range []string{"not-a-duration", "-10m", "0s"} {
		t.Setenv("FILE_EXPLODER_JOB_TIMEOUT", value)
		cfg := DefaultConfig()
		if cfg.JobTimeout != DefaultJobTimeout {
			t.Fatalf("FILE_EXPLODER_JOB_TIMEOUT=%q: JobTimeout = %v, want the default %v", value, cfg.JobTimeout, DefaultJobTimeout)
		}
	}
}

func TestEnsureDirsCreatesAMissingDirectory(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	cfg := &Config{DataDir: dataDir}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatalf("created directory mode = %v, want a 0700 directory", info.Mode())
	}
}

func TestEnsureDirsFixesAnOverlyPermissiveExistingDirectory(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{DataDir: dataDir}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("mode = %v, want 0700", info.Mode())
	}
}

// A data directory that is itself a symlink must be refused rather than
// silently followed: os.Stat/os.Chmod both dereference it, which would
// force whatever it points to down to 0700 - a directory the config's own
// path never named.
func TestEnsureDirsRefusesASymlinkedDataDirectory(t *testing.T) {
	tmp := t.TempDir()
	realDir := filepath.Join(tmp, "elsewhere")
	if err := os.Mkdir(realDir, 0755); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(tmp, "data")
	if err := os.Symlink(realDir, dataDir); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{DataDir: dataDir}
	if err := cfg.EnsureDirs(); err == nil {
		t.Fatal("EnsureDirs unexpectedly followed a symlinked data directory")
	}

	info, err := os.Lstat(realDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Fatalf("real target's mode changed to %v; EnsureDirs must not chmod through the symlink", info.Mode())
	}
}
