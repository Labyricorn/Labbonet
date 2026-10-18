package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Labyricorn/Labbonet/internal/interchange"
	"github.com/Labyricorn/Labbonet/internal/store"
)

// Data serves card/snapshot import, export and scoped reset. Imports are untrusted
// files: they are parsed and validated in full before any write, and previews run the
// real import inside a transaction that is rolled back.
type Data struct {
	Store   *store.Store
	DataDir string
}

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

func slug(name, fallback string) string {
	s := strings.Trim(slugRE.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-")
	}
	if s == "" {
		return fallback
	}
	return s
}

func download(w http.ResponseWriter, contentType, filename string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

type importResponse struct {
	Format    string             `json:"format"`
	Items     []interchange.Item `json:"items"`
	Threads   int                `json:"conversations"`
	Messages  int                `json:"messages"`
	Result    store.ImportResult `json:"result"`
	Committed bool               `json:"committed"`
}

// Import handles POST /api/data/import?mode=preview|commit&policy=add|skip&defaults=1.
func (d *Data) Import(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	mode, policy := q.Get("mode"), q.Get("policy")
	if (mode != "preview" && mode != "commit") || (policy != "add" && policy != "skip") {
		http.Error(w, "mode must be preview or commit and policy must be add or skip", 400)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, interchange.MaxUpload)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "file is unreadable or larger than 64 MiB", http.StatusRequestEntityTooLarge)
		return
	}
	parsed, err := interchange.Parse(data)
	if err != nil {
		http.Error(w, "import rejected: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}
	res, err := d.Store.ImportSnapshot(r.Context(), parsed.Snapshot, store.ImportOptions{Policy: policy, ApplyDefaults: q.Get("defaults") == "1", DryRun: mode == "preview"})
	if err != nil {
		if errors.Is(err, store.ErrInvalidImport) {
			http.Error(w, "import rejected: "+err.Error(), http.StatusUnprocessableEntity)
		} else {
			http.Error(w, "import failed; nothing was changed", 500)
		}
		return
	}
	msgs := 0
	for _, c := range parsed.Snapshot.Conversations {
		msgs += len(c.Messages)
	}
	writeJSON(w, 200, importResponse{Format: parsed.Format, Items: parsed.Items, Threads: len(parsed.Snapshot.Conversations), Messages: msgs, Result: res, Committed: mode == "commit"})
}

// Export handles GET /api/data/export?scope=full|cards|conversation&id=N.
func (d *Data) Export(w http.ResponseWriter, r *http.Request) {
	scope := r.URL.Query().Get("scope")
	var id int64
	if scope == "conversation" {
		var err error
		if id, err = strconv.ParseInt(r.URL.Query().Get("id"), 10, 64); err != nil || id <= 0 {
			http.Error(w, "invalid conversation id", 400)
			return
		}
	}
	snap, err := d.Store.ExportSnapshot(r.Context(), scope, id)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			http.Error(w, "conversation not found", 404)
		case errors.Is(err, store.ErrInvalidImport):
			http.Error(w, "scope must be full, cards, threads or conversation", 400)
		default:
			http.Error(w, "export failed", 500)
		}
		return
	}
	body, err := marshalIndented(snap)
	if err != nil {
		http.Error(w, "export failed", 500)
		return
	}
	name := fmt.Sprintf("labbonet-%s-%s.json", scope, time.Now().Format("20060102-150405"))
	download(w, "application/json; charset=utf-8", name, body)
}

// ExportCard handles GET /api/cast/{id}/export?format=native|v3|v2|png|zip.
func (d *Data) ExportCard(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	card, err := d.Store.GetCard(r.Context(), id)
	if err != nil {
		castError(w, err)
		return
	}
	base := slug(card.Name, "card")
	var body []byte
	switch f := r.URL.Query().Get("format"); f {
	case "native":
		download(w, "application/json; charset=utf-8", base+".labbonet-card.json", interchange.ExportNative(*card))
		return
	case "v3":
		download(w, "application/json; charset=utf-8", base+".v3.json", interchange.ExportV3JSON(*card))
		return
	case "v2":
		download(w, "application/json; charset=utf-8", base+".v2.json", interchange.ExportV2JSON(*card))
		return
	case "png":
		body, err = interchange.ExportPNG(*card)
		if err == nil {
			download(w, "image/png", base+".png", body)
			return
		}
	case "zip":
		body, err = interchange.ExportZIP(*card)
		if err == nil {
			download(w, "application/zip", base+".charx", body)
			return
		}
	default:
		http.Error(w, "format must be native, v3, v2, png or zip", 400)
		return
	}
	http.Error(w, "export failed", 500)
}

// Reset handles POST /api/data/reset. The request must carry the exact confirmation
// phrase; a credential-free backup is written first and reset aborts if it fails.
func (d *Data) Reset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scope   string `json:"scope"`
		Confirm string `json:"confirm"`
	}
	if !decodeCast(w, r, &req) {
		return
	}
	if req.Confirm != "RESET "+strings.ToUpper(req.Scope) {
		http.Error(w, `confirmation phrase must be "RESET " followed by the scope in capitals`, 400)
		return
	}
	if req.Scope != "conversations" && req.Scope != "cast" && req.Scope != "all" {
		http.Error(w, "scope must be conversations, cast or all", 400)
		return
	}
	path, err := d.Store.Backup(r.Context(), d.DataDir)
	if err != nil {
		http.Error(w, "reset aborted: the pre-reset backup failed", 500)
		return
	}
	if err = d.Store.ResetData(r.Context(), req.Scope); err != nil {
		http.Error(w, "reset failed; no data was removed", 500)
		return
	}
	writeJSON(w, 200, map[string]string{"backup": path, "scope": req.Scope})
}

func marshalIndented(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }
