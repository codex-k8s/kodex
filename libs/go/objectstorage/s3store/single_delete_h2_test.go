package s3store

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

// TestDeleteOnceDoesNotReplayHTTP2RefusedStream counts actual TLS HTTP/2 request HEADERS after
// the peer sends REFUSED_STREAM. A second request would succeed, exposing hidden transport retry.
// TestDeleteOnceDoesNotReplayHTTP2RefusedStream считает actual TLS HTTP/2 request HEADERS после
// peer REFUSED_STREAM. Второй request успешен, поэтому выявляет hidden transport retry.
func TestDeleteOnceDoesNotReplayHTTP2RefusedStream(t *testing.T) {
	certificate := httptest.NewTLSServer(http.NotFoundHandler())
	cert := certificate.TLS.Certificates[0]
	roots := x509.NewCertPool()
	roots.AddCert(certificate.Certificate())
	certificate.Close()
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("open synthetic TLS endpoint")
	}
	listener := tls.NewListener(raw, &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, NextProtos: []string{"h2"}})
	accepted := make(chan net.Conn, 1)
	joined := make(chan struct{})
	var requests atomic.Int64
	go func() {
		defer close(joined)
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		accepted <- connection
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
		preface := make([]byte, 24)
		if _, err := io.ReadFull(connection, preface); err != nil || string(preface) != "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n" {
			return
		}
		if err := writeSyntheticH2Frame(connection, 4, 0, 0, nil); err != nil {
			return
		}
		for {
			header := make([]byte, 9)
			if _, err := io.ReadFull(connection, header); err != nil {
				return
			}
			length := int(header[0])<<16 | int(header[1])<<8 | int(header[2])
			if length > 65536 {
				return
			}
			payload := make([]byte, length)
			if _, err := io.ReadFull(connection, payload); err != nil {
				return
			}
			stream := binary.BigEndian.Uint32(header[5:]) & 0x7fffffff
			if header[3] == 4 && header[4]&1 == 0 {
				if writeSyntheticH2Frame(connection, 4, 1, 0, nil) != nil {
					return
				}
			}
			if header[3] != 1 {
				continue
			}
			if requests.Add(1) == 1 {
				reset := make([]byte, 4)
				binary.BigEndian.PutUint32(reset, 7)
				if writeSyntheticH2Frame(connection, 3, 0, stream, reset) != nil {
					return
				}
			} else {
				// HPACK static index9 is :status204; END_HEADERS|END_STREAM closes the response.
				// HPACK static index9 означает :status204; END_HEADERS|END_STREAM закрывает response.
				if writeSyntheticH2Frame(connection, 1, 5, stream, []byte{0x89}) != nil {
					return
				}
			}
		}
	}()
	defer func() {
		_ = listener.Close()
		select {
		case connection := <-accepted:
			_ = connection.Close()
		default:
		}
		select {
		case <-joined:
		case <-time.After(6 * time.Second):
			t.Error("synthetic HTTP/2 peer did not join")
		}
	}()
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}, ForceAttemptHTTP2: true}
	defer transport.CloseIdleConnections()
	store, err := New(t.Context(), Config{Endpoint: "https://" + listener.Addr().String(), Region: "us-east-1", Bucket: "synthetic", AccessKeyID: "synthetic-key", SecretKey: "synthetic-secret", UsePathStyle: true, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal("synthetic S3 setup failed")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	err = store.DeleteOnce(ctx, "synthetic/refused-stream", "exact-version")
	if !errors.Is(err, objectstorage.ErrUnavailable) || requests.Load() != 1 {
		t.Fatalf("HTTP/2 silently replayed DELETE: failed=%t requests=%d", err != nil, requests.Load())
	}
}

// writeSyntheticH2Frame writes one bounded fixture frame without additional protocol libraries.
// writeSyntheticH2Frame пишет один bounded fixture frame без дополнительных protocol libraries.
func writeSyntheticH2Frame(writer io.Writer, kind, flags byte, stream uint32, payload []byte) error {
	header := make([]byte, 9)
	header[0] = byte(len(payload) >> 16)
	header[1] = byte(len(payload) >> 8)
	header[2] = byte(len(payload))
	header[3] = kind
	header[4] = flags
	binary.BigEndian.PutUint32(header[5:], stream)
	if _, err := writer.Write(header); err != nil {
		return err
	}
	_, err := writer.Write(payload)
	return err
}
