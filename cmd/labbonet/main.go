// Command labbonet is the Labbonet backend: a loopback-only HTTP server
// that serves the frontend and the /api handlers.
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Labyricorn/Labbonet/frontend"
	"github.com/Labyricorn/Labbonet/internal/api"
	"github.com/Labyricorn/Labbonet/internal/egress"
	"github.com/Labyricorn/Labbonet/internal/generate"
	"github.com/Labyricorn/Labbonet/internal/secrets"
	"github.com/Labyricorn/Labbonet/internal/server"
	"github.com/Labyricorn/Labbonet/internal/store"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "labbonet:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("labbonet", flag.ContinueOnError)
	port := flags.Int("port", 4815, "TCP port to listen on (loopback only)")
	host := flags.String("host", "127.0.0.1", "address to bind (must be 127.0.0.1)")
	dataDir := flags.String("data-dir", defaultDataDir(), "directory for the database, backups, logs and lock file")
	frontendDir := flags.String("frontend-dir", "", "serve the frontend from this directory instead of the embedded copy (development only)")
	open := flags.Bool("open", false, "open the launch URL in the default browser on start")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if *host != "127.0.0.1" {
		return fmt.Errorf("--host must be 127.0.0.1; Labbonet never listens on a non-loopback address")
	}

	lock, err := server.AcquireDataDirLock(*dataDir)
	if err != nil {
		return err
	}
	defer lock.Release()

	sessions, err := server.NewSessionManager()
	if err != nil {
		return fmt.Errorf("create session manager: %w", err)
	}

	st, err := store.Open(filepath.Join(*dataDir, "labbonet.db"))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()

	logger, closeLog, err := openAccessLogger(*dataDir)
	if err != nil {
		return err
	}
	defer closeLog()

	var frontendFS fs.FS
	if *frontendDir != "" {
		frontendFS = server.DirFrontend(*frontendDir)
	} else {
		frontendFS = frontend.FS
	}

	srv, err := server.New(server.Config{
		Host:     *host,
		Port:     *port,
		Frontend: frontendFS,
		Sessions: sessions,
		Logger:   logger,
	})
	if err != nil {
		return err
	}
	srv.Handle("GET /api/health", http.HandlerFunc(api.Health))
	srv.Handle("POST /api/backup", api.BackupHandler(st, *dataDir))
	data := &api.Data{Store: st, DataDir: *dataDir}
	srv.Handle("POST /api/data/import", http.HandlerFunc(data.Import))
	srv.Handle("GET /api/data/export", http.HandlerFunc(data.Export))
	srv.Handle("POST /api/data/reset", http.HandlerFunc(data.Reset))
	srv.Handle("GET /api/cast/{id}/export", http.HandlerFunc(data.ExportCard))

	protector := secrets.NewProtector()
	endpoints := &api.Endpoints{Store: st, Protector: protector}
	srv.Handle("GET /api/endpoints", http.HandlerFunc(endpoints.List))
	srv.Handle("POST /api/endpoints", http.HandlerFunc(endpoints.Create))
	srv.Handle("PATCH /api/endpoints/{id}", http.HandlerFunc(endpoints.Patch))
	srv.Handle("DELETE /api/endpoints/{id}", http.HandlerFunc(endpoints.Delete))
	srv.Handle("PUT /api/endpoints/{id}/credential", http.HandlerFunc(endpoints.SetCredential))
	srv.Handle("DELETE /api/endpoints/{id}/credential", http.HandlerFunc(endpoints.ClearCredential))

	egressClient := egress.NewClient()
	models := &api.Models{Store: st, Protector: protector, Egress: egressClient}
	srv.Handle("POST /api/endpoints/{id}/test", http.HandlerFunc(models.Test))
	srv.Handle("POST /api/endpoints/{id}/models/refresh", http.HandlerFunc(models.Refresh))
	srv.Handle("GET /api/endpoints/{id}/models", http.HandlerFunc(models.List))

	conversations := &api.Conversations{Store: st}
	srv.Handle("GET /api/conversations", http.HandlerFunc(conversations.List))
	srv.Handle("POST /api/conversations", http.HandlerFunc(conversations.Create))
	srv.Handle("GET /api/conversations/{id}", http.HandlerFunc(conversations.Get))
	srv.Handle("PATCH /api/conversations/{id}", http.HandlerFunc(conversations.Patch))
	srv.Handle("DELETE /api/conversations/{id}", http.HandlerFunc(conversations.Delete))
	cast := &api.Cast{Store: st}
	srv.Handle("GET /api/cast", http.HandlerFunc(cast.List))
	srv.Handle("POST /api/cast", http.HandlerFunc(cast.Save))
	srv.Handle("PUT /api/cast/order", http.HandlerFunc(cast.Reorder))
	srv.Handle("PUT /api/cast/{id}", http.HandlerFunc(cast.Save))
	srv.Handle("DELETE /api/cast/{id}", http.HandlerFunc(cast.Delete))
	srv.Handle("GET /api/cast/defaults", http.HandlerFunc(cast.Selection))
	srv.Handle("PUT /api/cast/defaults", http.HandlerFunc(cast.Selection))
	srv.Handle("GET /api/conversations/{id}/cast", http.HandlerFunc(cast.Selection))
	srv.Handle("PUT /api/conversations/{id}/cast", http.HandlerFunc(cast.Selection))
	srv.Handle("POST /api/conversations/{id}/greeting", http.HandlerFunc(cast.Greet))

	registry := generate.NewRegistry()
	runner := &generate.Runner{Store: st, Protector: protector, Egress: egressClient, Slots: generate.NewSlots(), Registry: registry}
	gen := &api.Generate{Runner: runner}
	srv.Handle("POST /api/conversations/{id}/generate", http.HandlerFunc(gen.Generate))
	srv.Handle("POST /api/generations/{id}/cancel", http.HandlerFunc(gen.Cancel))

	diagnostics := &api.Diagnostics{Store: st}
	srv.Handle("GET /api/diagnostics", http.HandlerFunc(diagnostics.List))

	ln, err := srv.Listen()
	if err != nil {
		return err
	}

	launchURL := srv.LaunchURL()
	fmt.Printf("Labbonet is listening on http://%s:%d\n", *host, *port)
	fmt.Printf("Open this URL once to authenticate the browser:\n\n  %s\n\n", launchURL)

	if *open {
		openBrowser(launchURL)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	// ARCHITECTURE.md section 6: "cancel running generations (partial
	// text saved)". http.Server.Shutdown alone only waits for in-flight
	// handlers to return; it never cancels their contexts, so a
	// streaming request would otherwise hold the shutdown open until
	// the upstream provider itself finishes or the client disconnects.
	registry.CancelAll()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// openAccessLogger opens dataDir/logs/access.log for append, creating
// the logs directory if needed, and returns a logger plus a close
// function. The access log never carries headers or bodies (see
// internal/server.AccessLog), so it can never leak conversation
// content or a credential (SECURITY-MODEL.md section 7).
func openAccessLogger(dataDir string) (*log.Logger, func(), error) {
	logsDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("create logs directory: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(logsDir, "access.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open access log: %w", err)
	}
	logger := log.New(f, "", log.LstdFlags)
	return logger, func() { f.Close() }, nil
}

func defaultDataDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "Labbonet")
}
