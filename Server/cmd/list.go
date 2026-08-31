package cmd

import (
	"bufio"
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
// single symlink's target type.
const symlinkResolveTimeout = 2 * time.Second

// symlinkResolveBudget bounds the *total* time listDirectory spends
// resolving symlink targets across a whole directory. Without it, a
// directory containing many symlinks into the same hung mount would each pay
// up to symlinkResolveTimeout in turn, making the command's latency scale
// with the symlink count instead of staying capped once no matter how large
// the directory is.
const symlinkResolveBudget = 20 * time.Second

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
	return newFileInfoWithTimeout(path, name, info, symlinkResolveTimeout)
}

// newFileInfoWithTimeout is newFileInfo with an explicit symlink-resolution
// timeout, so listDirectory can spend down a shared total budget across many
// entries instead of paying the full symlinkResolveTimeout on every one.
func newFileInfoWithTimeout(path, name string, info os.FileInfo, timeout time.Duration) FileInfo {
	isSymlink := info.Mode()&os.ModeSymlink != 0
	isDir := info.IsDir()
	if isSymlink {
		if target, ok := statWithTimeout(path, timeout); ok {
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
	// NewTimer + Stop, not time.After: a listing calls this once per symlink,
	// and a time.After timer stays armed in the runtime's timer heap for its
	// full duration even after the stat has already answered. A directory of
	// symlinks that all resolve instantly would still leave one live
	// symlinkResolveTimeout timer per entry behind it, all of them holding a
	// channel nobody will ever read.
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case info := <-result:
		return info, info != nil
	case <-timer.C:
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

	// Buffered so writeJSONArray's per-element writes reach stdout as a small
	// number of large writes instead of one write(2) syscall per element (and
	// another per separator) - the streaming encode below is about memory, not
	// about how many syscalls it costs to hand the bytes to the kernel.
	w := bufio.NewWriter(os.Stdout)
	if err := writeJSONArray(w, results); err != nil {
		return err
	}
	return w.Flush()
}

// writeJSONArray writes items as a JSON array one element at a time, instead
// of building the whole array as a single encoded byte buffer the way
// json.Encoder.Encode(items) would. ReadDir already requires holding every
// entry of a directory in memory at once, so this does not change that, but
// a very large listing no longer also needs its entire JSON encoding held
// in memory simultaneously alongside it. Callers that care about syscall
// count (runList does) should wrap w in a buffered writer themselves.
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

// timeoutForEntry returns the smaller of symlinkResolveTimeout and however
// much of the shared per-directory budget (deadline) is left, so a single
// entry never waits past whichever bound is tighter. Once the budget is
// exhausted this is zero or negative, which statWithTimeout treats as an
// immediate timeout - so an entry past the budget still gets listed, just
// without waiting to resolve its symlink target.
func timeoutForEntry(deadline time.Time) time.Duration {
	remaining := time.Until(deadline)
	if remaining > symlinkResolveTimeout {
		return symlinkResolveTimeout
	}
	return remaining
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

	deadline := time.Now().Add(symlinkResolveBudget)
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

		results = append(results, newFileInfoWithTimeout(filepath.Join(target, entry.Name()), entry.Name(), info, timeoutForEntry(deadline)))
	}

	return results, nil
}
