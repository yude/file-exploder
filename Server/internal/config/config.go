package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	DBPath  string
	LogPath string
	DataDir string
}

func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".file-exploder")
	return &Config{
		DBPath:  filepath.Join(dataDir, "queue.db"),
		LogPath: filepath.Join(dataDir, "queue.log"),
		DataDir: dataDir,
	}
}

func (c *Config) EnsureDirs() error {
	return os.MkdirAll(c.DataDir, 0700)
}
