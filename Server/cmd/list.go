package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

// symlinkResolveTimeout bounds how long newFileInfo waits to resolve a
// symlink's target type.
const symlinkResolveTimeout = 2 * time.Second

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
		if target, ok := statWithTimeout(path, symlinkResolveTimeout); ok {
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

// statWithTimeout resolves a symlink's target with os.Stat, but gives up
// after timeout instead of blocking forever - the same way a dangling
// symlink (target does not exist) already falls back to reporting the link
// itself. A symlink into a stale "hard" NFS/SMB/FUSE mount blocks the stat(2)
// syscall in uninterruptible sleep, a state nothing in Go can cancel, so the
// goroutine started here is abandoned rather than joined if it does not
// return in time: it can only ever be reclaimed by the mount recovering or
// this process exiting, but at least this and every other entry stop waiting
// on it.
func statWithTimeout(path string, timeout time.Duration) (os.FileInfo, bool) {
	return statWithTimeoutUsing(os.Stat, path, timeout)
}

func statWithTimeoutUsing(stat func(string) (os.FileInfo, error), path string, timeout time.Duration) (os.FileInfo, bool) {
	result := make(chan os.FileInfo, 1)
	go func() {
		info, err := stat(path)
		if err != nil {
			info = nil
		}
		result <- info
	}()
	select {
	case info := <-result:
		return info, info != nil
	case <-time.After(timeout):
		return nil, false
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

	return writeJSONArray(os.Stdout, results)
}

// writeJSONArray writes items as a JSON array one element at a time, instead
// of building the whole array as a single encoded byte buffer the way
// json.Encoder.Encode(items) would. ReadDir already requires holding every
// entry of a directory in memory at once, so this does not change that, but
// a very large listing no longer also needs its entire JSON encoding held
// in memory simultaneously alongside it.
func writeJSONArray(w io.Writer, items []FileInfo) error {
	if _, err := io.WriteString(w, "["); err != nil {
		return err
	}
	for i, item := range items {
		if i > 0 {
			if _, err := io.WriteString(w, ","); err != nil {
				return err
			}
		}
		data, err := json.Marshal(item)
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "]\n")
	return err
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
