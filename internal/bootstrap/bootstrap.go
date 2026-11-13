// Package bootstrap reads the service's bootstrap config file, the path the
// operator gives in BOOTSTRAP_CONFIG.
package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
)

// Read returns the contents of the bootstrap config file at path. The path
// must be absolute and already clean (no "..", "." or doubled separators), so
// what is read is exactly the file it names; the file is opened through an
// os.Root on its directory, so nothing outside that directory is read.
func Read(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("bootstrap config path %q is not absolute", path)
	}
	clean := filepath.Clean(path)
	if clean != path {
		return nil, fmt.Errorf("bootstrap config path %q is not clean (want %q)", path, clean)
	}
	root, err := os.OpenRoot(filepath.Dir(clean))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.ReadFile(filepath.Base(clean))
}
