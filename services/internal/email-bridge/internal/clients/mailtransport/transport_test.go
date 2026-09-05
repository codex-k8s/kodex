package mailtransport

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestTunnelCancellationDuringCONNECT(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		_, err = http.ReadRequest(bufio.NewReader(conn))
		cancel()
		if err != nil {
			serverDone <- err
			return
		}
		var b [1]byte
		_, err = conn.Read(b[:])
		if err == io.EOF {
			err = nil
		}
		serverDone <- err
	}()
	start := time.Now()
	conn, err := (Tunnel{Address: listener.Addr().String()}).Dial(ctx, "mail.example.test:993")
	if conn != nil {
		conn.Close()
	}
	if err == nil || time.Since(start) >= time.Second {
		t.Error("CONNECT wait did not stop promptly after cancellation")
	}
	if err := <-serverDone; err != nil {
		t.Fatal("fixture did not receive CONNECT")
	}
}
