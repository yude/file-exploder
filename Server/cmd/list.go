package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

type FileInfo struct {
	Name             string `json:"name"`
	Path             string `json:"path"`
	Size             int64  `json:"size"`
	ModificationDate int64  `json:"modificationDate"`
	IsDirectory      bool   `json:"isDirectory"`
	IsSymlink        bool   `json:"isSymlink"`
	Permissions      uint32 `json:"permissions"`
}

// newFileInfo describes a path from its lstat result. Symbolic links are
// reported as links, but IsDirectory follows the link so the client can descend
// into symlinked directories; a link whose target is gone stays a plain entry.
func newFileInfo(path, name string, info os.FileInfo) FileInfo {
	isSymlink := info.Mode()&os.ModeSymlink != 0
	isDir := info.IsDir()
	if isSymlink {
		if target, err := os.Stat(path); err == nil {
			isDir = target.IsDir()
		}
	}

	return FileInfo{
		Name:             name,
		Path:             path,
		Size:             info.Size(),
		ModificationDate: info.ModTime().Unix(),
		IsDirectory:      isDir,
		IsSymlink:        isSymlink,
		Permissions:      uint32(info.Mode() & os.ModePerm),
	}
}

var listCmd = &cobra.Command{
	Use:   "list [path]",
	Short: "List directory contents safely as JSON",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runList,
}

var listPathBase64 string

func init() {
	listCmd.Flags().StringVar(&listPathBase64, "path-base64", "", "Path encoded as UTF-8 Base64")
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	plainPath := ""
	if len(args) == 1 {
		plainPath = args[0]
	}
	target, err := decodePathFlag("path", plainPath, listPathBase64)
	if err != nil {
		return err
	}
	if target == "" {
		return fmt.Errorf("a path or --path-base64 is required")
	}

	results, err := listDirectory(target)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(results)
}

func listDirectory(target string) ([]FileInfo, error) {
	// Entry names are checked below, but every path in the response is built by
	// joining them onto this target - so an unrepresentable target alone is
	// enough to hand the client the U+FFFD alias this guard exists to prevent.
	// stat already refuses such a path; list has to agree.
	if !utf8.ValidString(target) {
		return nil, fmt.Errorf("path cannot be represented safely as UTF-8")
	}

	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}

	results := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		// JSON strings must be valid UTF-8. encoding/json replaces invalid bytes
		// with U+FFFD, which would make the client send later operations to a
		// different pathname. Such entries cannot be represented safely by the
		// Swift client, so leave them out rather than expose a destructive alias.
		if !utf8.ValidString(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			// The entry was removed between ReadDir and Info; skip it rather
			// than failing the whole listing over a concurrent change.
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}

		results = append(results, newFileInfo(filepath.Join(target, entry.Name()), entry.Name(), info))
	}

	return results, nil
}
