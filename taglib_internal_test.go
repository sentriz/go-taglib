package taglib

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestNewRuntimeUnwritableCacheDir ensures that creating a new runtime with an unwritable
// cache directory does not fail (since it should fall back to in-memory caching).
func TestNewRuntimeUnwritableCacheDir(t *testing.T) {
	t.Parallel()

	// nothing can be created below a regular file
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := newRuntime(filepath.Join(file, "cache"))
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	t.Cleanup(func() { _ = rt.Runtime.Close(context.Background()) })
}
