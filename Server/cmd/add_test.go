package cmd

import (
	"encoding/base64"
	"path/filepath"
	"testing"
)

func TestDecodePathFlagPreservesUnicodeNormalization(t *testing.T) {
	composed := "/mnt/store3/聞きたいこと\u304cある.m2ts"
	decomposed := "/mnt/store3/聞きたいこと\u304b\u3099ある.m2ts"
	encoded := base64.StdEncoding.EncodeToString([]byte(composed))

	got, err := decodePathFlag("src", "", encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got != composed {
		t.Fatalf("decoded path = %q, want byte-identical %q", got, composed)
	}
	if got == decomposed {
		t.Fatal("decoded path was changed to a canonically equivalent normalization")
	}
}

func TestDecodePathFlagRejectsAmbiguousOrInvalidInput(t *testing.T) {
	if _, err := decodePathFlag("src", "/plain", "L2VuY29kZWQ="); err == nil {
		t.Fatal("accepted both plain and encoded source paths")
	}
	if _, err := decodePathFlag("src", "", "not base64"); err == nil {
		t.Fatal("accepted invalid Base64")
	}
	invalidUTF8 := base64.StdEncoding.EncodeToString([]byte{0xff})
	if _, err := decodePathFlag("src", "", invalidUTF8); err == nil {
		t.Fatal("accepted a path that is not valid UTF-8")
	}
}

func TestGuardDataDirRefusesOperationsOnTheQueuesOwnState(t *testing.T) {
	dataDir := "/home/user/.file-exploder"

	refused := []string{
		dataDir,                            // the directory itself
		filepath.Join(dataDir, "queue.db"), // the database alone is enough
		"/home/user",                       // an ancestor takes it with them
		"/home",                            //
		"/home/user/.file-exploder/../.file-exploder", // an equivalent spelling
	}
	for _, path := range refused {
		if err := guardDataDir(dataDir, path, ""); err == nil {
			t.Errorf("guardDataDir allowed %q", path)
		}
		if err := guardDataDir(dataDir, "", path); err == nil {
			t.Errorf("guardDataDir allowed %q as a destination", path)
		}
	}

	allowed := []string{
		"/home/user2",
		"/home/user-other/.file-exploder",
		"/srv/data",
		"/home/user/documents",
		"/home/user/.file-exploder-backup", // a sibling that merely shares a prefix
	}
	for _, path := range allowed {
		if err := guardDataDir(dataDir, path, ""); err != nil {
			t.Errorf("guardDataDir refused %q: %v", path, err)
		}
	}

	if err := guardDataDir(dataDir, "", ""); err != nil {
		t.Errorf("guardDataDir refused an empty operation: %v", err)
	}
}
