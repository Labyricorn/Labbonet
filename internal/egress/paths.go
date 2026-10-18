package egress

import "fmt"

// AllowedPaths is the fixed set of request paths the egress client may
// use (PROVIDER-SPEC.md section 1.5, SECURITY-MODEL.md section 5). User
// input, model output and imported content never form any part of a
// request path; model IDs travel in JSON bodies only.
var AllowedPaths = map[string]bool{
	"/v1/models":           true,
	"/v1/chat/completions": true,
	"/v1/embeddings":       true,
	"/health":              true,
	"/status":              true,
	"/api/v0/models":       true,
}

// CheckPath refuses any path outside AllowedPaths.
func CheckPath(path string) error {
	if !AllowedPaths[path] {
		return fmt.Errorf("egress: path %q is not in the allowed path list", path)
	}
	return nil
}
