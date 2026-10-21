//go:build linux

package udscred

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// TestCredentialsBindExactUnixPeer verifies that both handshake directions
// accept the actual kernel UID/GID on a real local Unix socket.
// RU: TestCredentialsBindExactUnixPeer проверяет, что обе стороны рукопожатия
// принимают фактические UID/GID ядра на настоящем локальном Unix-сокете.
func TestCredentialsBindExactUnixPeer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peer.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	serverResult := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverResult <- acceptErr
			return
		}
		defer connection.Close()
		_, _, handshakeErr := New(
			uint32(os.Getuid()),
			uint32(os.Getgid()),
		).ServerHandshake(connection)
		serverResult <- handshakeErr
	}()

	connection, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer connection.Close()
	if _, _, err := New(
		uint32(os.Getuid()),
		uint32(os.Getgid()),
	).ClientHandshake(context.Background(), "", connection); err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	if err := <-serverResult; err != nil {
		t.Fatalf("server handshake: %v", err)
	}
}

// TestCredentialsRejectWrongUID verifies closed rejection when the configured
// peer UID differs from the actual client UID on a Unix socket.
// RU: TestCredentialsRejectWrongUID проверяет закрытый отказ, если заданный
// UID peer отличается от фактического UID клиента Unix-сокета.
func TestCredentialsRejectWrongUID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peer.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	serverResult := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverResult <- acceptErr
			return
		}
		defer connection.Close()
		_, _, handshakeErr := New(
			uint32(os.Getuid()+1),
			uint32(os.Getgid()),
		).ServerHandshake(connection)
		serverResult <- handshakeErr
	}()
	connection, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer connection.Close()
	if err := <-serverResult; err == nil {
		t.Fatal("wrong peer UID accepted")
	}
}
