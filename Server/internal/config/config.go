package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	DBPath   string
	LogPath  string
	LockPath string
	DataDir  string
}

func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".file-exploder")
	envDir := os.Getenv("FILE_EXPLODER_DATA_DIR")
	if envDir != "" {
		dataDir = envDir
	}

	return &Config{
		DBPath:   filepath.Join(dataDir, "queue.db"),
		LogPath:  filepath.Join(dataDir, "queue.log"),
		LockPath: filepath.Join(dataDir, "daemon.lock"),
		DataDir:  dataDir,
	}
}

func (c *Config) EnsureDirs() error {
	if !filepath.IsAbs(c.DataDir) {
		return &os.PathError{Op: "configure", Path: c.DataDir, Err: os.ErrInvalid}
	}
	info, err := os.Stat(c.DataDir)
	if os.IsNotExist(err) {
		return os.MkdirAll(c.DataDir, 0700)
	}
	if err != nil {
		return err
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
