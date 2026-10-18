// Package migrations embeds the Labbonet SQL schema migrations so the
// binary is self-contained. Files are named "NNNN_description.sql" and
// applied in numeric order by internal/store.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
