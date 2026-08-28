package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DefaultJobTimeout bounds how long the daemon waits for a single job before
// abandoning it and moving on to the rest of the queue. It exists only to
// keep a job stuck on an unresponsive mount from wedging the whole queue
// forever, so it is deliberately generous - long enough that a large,
// legitimately slow copy over a slow link is never mistaken for a hang.
const DefaultJobTimeout = 24 * time.Hour

type Config struct {
	DBPath     string
	LogPath    string
	LockPath   string
	DataDir    string
	JobTimeout time.Duration
}

func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".file-exploder")
	envDir := os.Getenv("FILE_EXPLODER_DATA_DIR")
	if envDir != "" {
		dataDir = envDir
	}

	jobTimeout := DefaultJobTimeout
	if envTimeout := os.Getenv("FILE_EXPLODER_JOB_TIMEOUT"); envTimeout != "" {
		if parsed, err := time.ParseDuration(envTimeout); err == nil && parsed > 0 {
			jobTimeout = parsed
		}
	}

	return &Config{
		DBPath:     filepath.Join(dataDir, "queue.db"),
		LogPath:    filepath.Join(dataDir, "queue.log"),
		LockPath:   filepath.Join(dataDir, "daemon.lock"),
		DataDir:    dataDir,
		JobTimeout: jobTimeout,
	}
}

func (c *Config) EnsureDirs() error {
	if !filepath.IsAbs(c.DataDir) {
		return fmt.Errorf("data directory must be an absolute path, got %q: set FILE_EXPLODER_DATA_DIR, or make sure HOME is set", c.DataDir)
	}
	// Lstat, not Stat: this directory holds the SQLite queue and the daemon
	// lock, so its permissions matter for real. os.Stat and os.Chmod both
	// follow a symlink transparently, which would silently force whatever it
	// points to down to 0700 - unlike every other symlink-aware check in this
	// codebase (executor.go's executeChmod, for one, explicitly Lstats and
	// refuses rather than following the link).
	info, err := os.Lstat(c.DataDir)
	if os.IsNotExist(err) {
		return os.MkdirAll(c.DataDir, 0700)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("data directory %s is a symlink; point FILE_EXPLODER_DATA_DIR at the real directory instead of a link to it", c.DataDir)
	}
	if !info.IsDir() {
		return &os.PathError{Op: "mkdir", Path: c.DataDir, Err: os.ErrExist}
	}
	if info.Mode().Perm() != 0700 {
		if err := os.Chmod(c.DataDir, 0700); err != nil {
			return err
		}
	}
	return nil
}
