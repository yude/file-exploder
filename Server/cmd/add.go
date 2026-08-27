package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/yude/file-exploder/server/internal/config"
	"github.com/yude/file-exploder/server/internal/executor"
	"github.com/yude/file-exploder/server/internal/queue"
)

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a file operation to the queue",
	Args:  cobra.NoArgs,
	RunE:  runAdd,
}

var (
	addType      string
	addSrc       string
	addDst       string
	addSrcBase64 string
	addDstBase64 string
	addMode      string
)

func init() {
	addCmd.Flags().StringVar(&addType, "type", "", "Operation type: rename, move, delete, copy, mkdir, chmod")
	addCmd.Flags().StringVar(&addSrc, "src", "", "Source path")
	addCmd.Flags().StringVar(&addDst, "dst", "", "Destination path")
	addCmd.Flags().StringVar(&addSrcBase64, "src-base64", "", "Source path encoded as UTF-8 Base64")
	addCmd.Flags().StringVar(&addDstBase64, "dst-base64", "", "Destination path encoded as UTF-8 Base64")
	addCmd.Flags().StringVar(&addMode, "mode", "", "File mode (for chmod)")
	if err := addCmd.MarkFlagRequired("type"); err != nil {
		panic(err)
	}
	rootCmd.AddCommand(addCmd)
}

func runAdd(cmd *cobra.Command, args []string) error {
	srcPath, err := decodePathFlag("src", addSrc, addSrcBase64)
	if err != nil {
		return err
	}
	dstPath, err := decodePathFlag("dst", addDst, addDstBase64)
	if err != nil {
		return err
	}

	// Validate job type
	validTypes := map[string]bool{
		"rename": true, "move": true, "delete": true,
		"copy": true, "mkdir": true, "chmod": true,
	}
	if !validTypes[addType] {
		return fmt.Errorf("invalid operation type: %s", addType)
	}

	// Basic validation
	if (addType == "rename" || addType == "move" || addType == "copy") && (srcPath == "" || dstPath == "") {
		return fmt.Errorf("both --src and --dst are required for %s", addType)
	}
	if addType == "delete" && srcPath == "" {
		return fmt.Errorf("--src is required for delete")
	}
	if addType == "mkdir" && dstPath == "" {
		return fmt.Errorf("--dst is required for mkdir")
	}
	if addType == "chmod" {
		if dstPath == "" || addMode == "" {
			return fmt.Errorf("both --dst and --mode are required for chmod")
		}
		// Reject a bad mode here so it never reaches the queue, using the same
		// parser the executor will apply.
		if _, err := executor.ParseFileMode(addMode); err != nil {
			return err
		}
	}
	switch addType {
	case "rename", "move", "copy":
		if addMode != "" {
			return fmt.Errorf("--mode is not valid for %s", addType)
		}
	case "delete":
		if dstPath != "" || addMode != "" {
			return fmt.Errorf("--dst and --mode are not valid for delete")
		}
	case "mkdir":
		if srcPath != "" || addMode != "" {
			return fmt.Errorf("--src and --mode are not valid for mkdir")
		}
	case "chmod":
		if srcPath != "" {
			return fmt.Errorf("--src is not valid for chmod")
		}
	}
	for _, path := range []string{srcPath, dstPath} {
		if path == "" {
			continue
		}
		if !filepath.IsAbs(path) {
			return fmt.Errorf("paths must be absolute: %s", path)
		}
		if strings.ContainsRune(path, '\x00') {
			return fmt.Errorf("paths cannot contain NUL bytes")
		}
		if filepath.Clean(path) == "/" {
			return fmt.Errorf("refusing to queue an operation on the filesystem root")
		}
	}

	cfg := config.DefaultConfig()
	if err := guardDataDir(cfg.DataDir, srcPath, dstPath); err != nil {
		return err
	}
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}

	q, err := queue.NewSQLiteQueue(cfg.DBPath)
	if err != nil {
		return err
	}
	defer q.Close()

	job := &queue.Job{
		ID:        uuid.New().String(),
		Type:      queue.JobType(addType),
		SrcPath:   srcPath,
		DstPath:   dstPath,
		Mode:      addMode,
		Status:    queue.StatusPending,
		CreatedAt: time.Now(),
	}

	if err := q.AddJob(job); err != nil {
		return err
	}

	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(map[string]string{
		"id":     job.ID,
		"status": "pending",
	})
}

// decodePathFlag keeps remote path bytes out of the macOS Process argument
// conversion. Foundation may expose canonically equivalent Unicode using a
// different normalization form, while Linux filenames compare the UTF-8 bytes
// exactly. The client therefore transports paths as ASCII Base64 and decoding
// happens only after the command reaches Linux.
func decodePathFlag(name, plain, encoded string) (string, error) {
	if plain != "" && encoded != "" {
		return "", fmt.Errorf("--%s and --%s-base64 cannot be used together", name, name)
	}
	if encoded == "" {
		return plain, nil
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("invalid --%s-base64: %w", name, err)
	}
	if !utf8.Valid(decoded) {
		return "", fmt.Errorf("--%s-base64 is not valid UTF-8", name)
	}
	return string(decoded), nil
}

// guardDataDir refuses operations that would destroy the queue's own state.
//
// The data directory is an ordinary directory under $HOME, so it is reachable
// from the client's file list like any other. Deleting it through the queue
// unlinks the database out from under the running daemon: the daemon keeps
// writing to the unlinked file while the next CLI call creates a fresh one, so
// from that point every job is accepted, reported as pending, and silently
// never runs - with the history gone and the daemon lock file removed, so a
// second daemon can start too. Nothing in the client can tell the user any of
// that happened.
//
// Any ancestor of the directory has the same effect, so those are refused as
// well. This is a footgun guard, not a sandbox: the same operation over plain
// SSH is still the user's to make.
func guardDataDir(dataDir string, paths ...string) error {
	absDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return err
	}
	// Compare resolved paths. A lexical comparison was walked around by any
	// symlink on the way in - a link to the directory, or an operation reaching
	// it through a linked ancestor - and the guard is worth nothing if the one
	// path spelling that matters can be spelled another way.
	resolvedDataDir, err := executor.ResolveAllowMissing(absDataDir)
	if err != nil {
		return err
	}

	for _, path := range paths {
		if path == "" {
			continue
		}
		resolved, err := executor.ResolveAllowMissing(path)
		if err != nil {
			return err
		}
		if pathsOverlap(resolved, resolvedDataDir) {
			return fmt.Errorf("refusing to queue an operation on the file-exploder data directory: %s", resolvedDataDir)
		}
	}
	return nil
}

// pathsOverlap reports whether either path is the other, or contains it.
func pathsOverlap(a, b string) bool {
	return a == b || pathContains(a, b) || pathContains(b, a)
}

func pathContains(parent, child string) bool {
	return strings.HasPrefix(child, strings.TrimSuffix(parent, string(filepath.Separator))+string(filepath.Separator))
}
