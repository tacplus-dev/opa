package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadReadsAnAbsoluteCleanPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("proxy_policy: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := Read(path)
	if err != nil {
		t.Fatalf("Read(%q): %v", path, err)
	}
	if string(data) != "proxy_policy: {}\n" {
		t.Fatalf("Read(%q) = %q", path, data)
	}
}

func TestReadRejectsABadPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"config.yaml",                      // relative
		"../config.yaml",                   // relative, climbing
		dir + "/sub/../config.yaml",        // not clean: a ".." element
		dir + "//config.yaml",              // not clean
		dir + "/./config.yaml",             // not clean
		dir + "/",                          // a directory, not clean
		dir,                                // a directory
		filepath.Join(dir, "missing.yaml"), // absent
		filepath.Join(dir, "config.yaml") + "\x00.txt", // NUL byte
	} {
		if data, err := Read(path); err == nil {
			t.Errorf("Read(%q) = %q, want an error", path, data)
		}
	}
}
