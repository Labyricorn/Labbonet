package egress

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The search destination is a constant, and the provider path allowlist is untouched: a
// provider endpoint can never be asked for the search path.
func TestSearchDestinationIsFixedAndSeparateFromProviderPaths(t *testing.T) {
	c := NewSearchClient()
	if c.base.Scheme != "https" || c.base.Host != "ollama.com" || c.class != ClassExternal {
		t.Fatalf("production client = %v class %q", c.base, c.class)
	}
	if SearchPath != "/api/web_search" {
		t.Fatalf("search path = %q", SearchPath)
	}
	if err := CheckPath(SearchPath); err == nil {
		t.Error("the search path was added to the provider allowlist")
	}
}

// Only the production constructor may be used outside tests: nothing in cmd/ or internal/
// (outside this package) may build a client for another host.
func TestNothingOutsideEgressPointsTheSearchClientElsewhere(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	for _, dir := range []string{"cmd", "internal"} {
		_ = filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			if strings.HasPrefix(rel, "internal/egress/") {
				return nil
			}
			data, _ := os.ReadFile(path)
			if strings.Contains(string(data), "NewSearchClientFor") {
				t.Errorf("%s builds a search client for an arbitrary URL", rel)
			}
			return nil
		})
	}
}
