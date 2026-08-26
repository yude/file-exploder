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
	envDir := os.Getenv("FILE_EXPLODER_DATA_DIR")
	if envDir != "" {
		dataDir = envDir
	}

	return &Config{
		DBPath:  filepath.Join(dataDir, "queue.db"),
		LogPath: filepath.Join(dataDir, "queue.log"),
		DataDir: dataDir,
	}
}

func (c *Config) EnsureDirs() error {
	info, err := os.Stat(c.DataDir)
	if os.IsNotExist(err) {
		return os.MkdirAll(c.DataDir, 0700)
	}
	if err == nil && (info.Mode()&0777) != 0700 {
		os.Chmod(c.DataDir, 0700)
	}
	return err
}
