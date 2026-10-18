package egress

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestOnlyEgressMakesOutboundRequests greps cmd/ and internal/ (outside
// this package and test-helper packages that act as a browser-side
// client against our own test server) for the constructs that make an
// outbound HTTP request or raw dial, proving internal/egress really is
// Labbonet's only path to a provider host (SECURITY-MODEL.md section 5,
// PHASE1-PLAN.md step 3 done criteria).
func TestOnlyEgressMakesOutboundRequests(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine this file's path")
	}
	// this file: internal/egress/isolation_test.go
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))

	forbidden := []string{"http.Get(", "http.Post(", "http.Client{", "net.Dial("}
	skipDirSegments := []string{"/servertest/", "/storetest/", "/fakeprovider/"}

	for _, dir := range []string{"cmd", "internal"} {
		start := filepath.Join(root, dir)
		err := filepath.WalkDir(start, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			if strings.HasPrefix(rel, "internal/egress/") {
				return nil
			}
			for _, seg := range skipDirSegments {
				if strings.Contains("/"+rel, seg) {
					return nil
				}
			}

			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			content := string(data)
			for _, pattern := range forbidden {
				if strings.Contains(content, pattern) {
					t.Errorf("%s contains %q: only internal/egress may make outbound HTTP requests", rel, pattern)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", start, err)
		}
	}
}
