package server_test

import (
	"net"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/server"
)

// S7: a non-loopback --host value is refused before anything starts
// listening.
func TestNewRefusesNonLoopbackHost(t *testing.T) {
	sessions, err := server.NewSessionManager()
	if err != nil {
		t.Fatalf("new session manager: %v", err)
	}

	_, err = server.New(server.Config{Host: "0.0.0.0", Port: 4815, Sessions: sessions})
	if err == nil {
		t.Fatal("expected New to refuse host 0.0.0.0")
	}
}

// A-B2: starting on a port already in use fails with a clear error and
// does not disturb whatever is already listening there.
func TestListenOnBusyPortFailsCleanly(t *testing.T) {
	sessions, err := server.NewSessionManager()
	if err != nil {
		t.Fatalf("new session manager: %v", err)
	}

	holder, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer holder.Close()
	port := holder.Addr().(*net.TCPAddr).Port

	srv, err := server.New(server.Config{Host: "127.0.0.1", Port: port, Sessions: sessions})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	if _, err := srv.Listen(); err == nil {
		t.Fatal("expected Listen to fail on a busy port")
	}

	// The original listener must still be usable.
	conn, err := net.Dial("tcp", holder.Addr().String())
	if err != nil {
		t.Fatalf("original listener was disturbed: %v", err)
	}
	conn.Close()
}
