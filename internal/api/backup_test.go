package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/store/storetest"
)

func TestBackupHandler_WritesBackupAndReportsPath(t *testing.T) {
	st, dataDir := storetest.NewWithDir(t)

	req := httptest.NewRequest(http.MethodPost, "/api/backup", nil)
	rr := httptest.NewRecorder()

	BackupHandler(st, dataDir).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	var resp backupResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Path == "" {
		t.Fatalf("response has no path: %+v", resp)
	}
	if filepath.Dir(resp.Path) != filepath.Join(dataDir, "backups") {
		t.Fatalf("backup path %q not inside %q", resp.Path, filepath.Join(dataDir, "backups"))
	}
	if _, err := os.Stat(resp.Path); err != nil {
		t.Fatalf("backup file does not exist: %v", err)
	}
}
