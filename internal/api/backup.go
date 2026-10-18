package api

import (
	"encoding/json"
	"net/http"

	"github.com/Labyricorn/Labbonet/internal/store"
)

type backupResponse struct {
	Path string `json:"path"`
}

// BackupHandler handles POST /api/backup: writes a consistent backup of
// the database into dataDir/backups and reports its path.
func BackupHandler(st *store.Store, dataDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path, err := st.Backup(r.Context(), dataDir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(backupResponse{Path: path})
	}
}
