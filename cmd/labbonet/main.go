// Command labbonet is the Labbonet backend: a loopback-only HTTP server
// that serves the frontend and the /api handlers.
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Labyricorn/Labbonet/frontend"
	"github.com/Labyricorn/Labbonet/internal/api"
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
	})
	if err != nil {
		return err
	}
	srv.Handle("GET /api/health", http.HandlerFunc(api.Health))
	srv.Handle("POST /api/backup", api.BackupHandler(st, *dataDir))

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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func defaultDataDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "Labbonet")
}
