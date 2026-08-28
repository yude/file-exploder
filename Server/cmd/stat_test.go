package cmd

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

// runStat writes its result as JSON to stdout and reads its target via the
// package-level statPathBase64 var, set by cobra from the --path-base64
// flag; these tests only need to know whether it accepted the target, so
// stdout is pointed at the null device rather than captured.
func runStatWithPath(t *testing.T, path, encoded string) error {
	t.Helper()
	statPathBase64 = encoded
	t.Cleanup(func() { statPathBase64 = "" })

	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	origStdout := os.Stdout
	os.Stdout = devNull
	defer func() { os.Stdout = origStdout }()

	var args []string
	if path != "" {
		args = []string{path}
	}
	return runStat(statCmd, args)
}

func TestStatAcceptsAPlainPositionalPath(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(target, []byte("hi"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := runStatWithPath(t, target, ""); err != nil {
		t.Fatal(err)
	}
}

func TestStatAcceptsAPathBase64Flag(t *testing.T) {
	// The same macOS Unicode-normalization hazard add.go's --src-base64 and
	// list.go's --path-base64 exist for: a composed-form Japanese filename
	// coming from Foundation must reach os.Lstat with its exact Linux bytes.
	// decodePathFlag's own normalization guarantees are covered by
	// TestDecodePathFlagPreservesUnicodeNormalization in add_test.go; this
	// only needs to confirm stat.go actually routes through it.
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "聞きたいことがある.m2ts")
	if err := os.WriteFile(target, []byte("hi"), 0600); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(target))

	if err := runStatWithPath(t, "", encoded); err != nil {
		t.Fatal(err)
	}
}

func TestStatRejectsBothAPathAndPathBase64(t *testing.T) {
	if err := runStatWithPath(t, "/plain", base64.StdEncoding.EncodeToString([]byte("/encoded"))); err == nil {
		t.Fatal("stat with both a positional path and --path-base64 unexpectedly succeeded")
	}
}

func TestStatRequiresAPathOrPathBase64(t *testing.T) {
	if err := runStatWithPath(t, "", ""); err == nil {
		t.Fatal("stat with neither a path nor --path-base64 unexpectedly succeeded")
	}
}
