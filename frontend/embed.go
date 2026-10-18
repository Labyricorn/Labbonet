// Package frontend embeds the Phase 1 static site so the binary stays
// self-contained (ARCHITECTURE.md section 3).
package frontend

import "embed"

//go:embed index.html app.js app.css js vendor
var FS embed.FS
