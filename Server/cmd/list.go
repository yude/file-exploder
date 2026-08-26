package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

type FileInfo struct {
	Name             string `json:"name"`
	Path             string `json:"path"`
	Size             int64  `json:"size"`
	ModificationDate int64  `json:"modificationDate"`
	IsDirectory      bool   `json:"isDirectory"`
	Permissions      uint32 `json:"permissions"`
}

var listCmd = &cobra.Command{
	Use:   "list [path]",
	Short: "List directory contents safely as JSON",
	Args:  cobra.ExactArgs(1),
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	target := args[0]
	entries, err := os.ReadDir(target)
	if err != nil {
		return err
	}

	var results []FileInfo
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue // Skip files we can't stat
		}
		
		results = append(results, FileInfo{
			Name:             entry.Name(),
			Path:             filepath.Join(target, entry.Name()),
			Size:             info.Size(),
			ModificationDate: info.ModTime().Unix(),
			IsDirectory:      entry.IsDir(),
			Permissions:      uint32(info.Mode() & os.ModePerm),
		})
	}

	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(results)
}
