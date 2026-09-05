package gateway

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
)

func TestMailTunnelPreservesEndToEndTLSVerification(t *testing.T) {
	certificate, roots := mailCertificate(t)
	for _, mode := range []string{"implicit", "starttls"} {
		for _, trust := range []string{"valid", "wrong-ca"} {
			t.Run(mode+"/"+trust, func(t *testing.T) {
				port := 465
				if mode == "starttls" {
					port = 587
				}
				active := mailFixture(t, "smtp", mode, port)
				resolver := &fakeResolver{snapshot: dnsresolver.Snapshot{Addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}, ExpiresAt: time.Now().Add(time.Minute)}}
				dialer := &fakeDialer{peers: make(chan net.Conn, 1)}
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
				defer cancel()
				server, err := New(ctx, "unused", active, resolver, dialer, readyStub(true), newTestMetrics(t))
				if err != nil {
					t.Fatal(err)
				}
				serverSide, client := net.Pipe()
				defer client.Close()
				done := make(chan struct{})
				go func() { defer close(done); defer serverSide.Close(); server.handle(serverSide) }()
				_ = client.SetDeadline(time.Now().Add(3 * time.Second))
				if _, err := fmt.Fprintf(client, "CONNECT mail.example.test:%d HTTP/1.1\r\nHost: mail.example.test:%d\r\n\r\n", port, port); err != nil {
					t.Fatal(err)
				}
				reader := bufio.NewReader(client)
				if _, err := http.ReadResponse(reader, nil); err != nil {
					t.Fatal(err)
				}
				providerResult := make(chan error, 1)
				go func() {
					var upstream net.Conn
					select {
					case upstream = <-dialer.peers:
					case <-ctx.Done():
						providerResult <- ctx.Err()
						return
					}
					defer upstream.Close()
					_ = upstream.SetDeadline(time.Now().Add(3 * time.Second))
					if mode == "starttls" {
						if _, err := io.WriteString(upstream, "220 fixture\r\n"); err != nil {
							providerResult <- err
							return
						}
						line, err := bufio.NewReader(upstream).ReadString('\n')
						if err != nil || line != "STARTTLS\r\n" {
							providerResult <- fmt.Errorf("invalid upgrade: %w", err)
							return
						}
						if _, err := io.WriteString(upstream, "220 Ready\r\n"); err != nil {
							providerResult <- err
							return
						}
					}
					secured := tls.Server(upstream, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
					if err := secured.HandshakeContext(ctx); err != nil {
						providerResult <- err
						return
					}
					payload := make([]byte, 4)
					_, err := io.ReadFull(secured, payload)
					if err == nil && string(payload) != "ping" {
						err = fmt.Errorf("encrypted payload mismatch")
					}
					if err == nil {
						_, err = secured.Write([]byte("pong"))
					}
					providerResult <- err
				}()
				if mode == "starttls" {
					if line, err := reader.ReadString('\n'); err != nil || line != "220 fixture\r\n" {
						t.Fatal("greeting missing", err)
					}
					if _, err := io.WriteString(client, "STARTTLS\r\n"); err != nil {
						t.Fatal(err)
					}
					if line, err := reader.ReadString('\n'); err != nil || line != "220 Ready\r\n" {
						t.Fatal("upgrade response missing", err)
					}
				}
				pool := roots
				if trust == "wrong-ca" {
					pool = x509.NewCertPool()
				}
				secured := tls.Client(client, &tls.Config{RootCAs: pool, ServerName: "mail.example.test", MinVersion: tls.VersionTLS12})
				err = secured.HandshakeContext(ctx)
				if trust == "valid" {
					if err != nil {
						t.Fatal(err)
					}
					if _, err := secured.Write([]byte("ping")); err != nil {
						t.Fatal(err)
					}
					payload := make([]byte, 4)
					if _, err := io.ReadFull(secured, payload); err != nil || string(payload) != "pong" {
						t.Fatal("encrypted response mismatch", err)
					}
				} else if err == nil {
					t.Fatal("unknown CA accepted through mail tunnel")
				}
				_ = client.Close()
				select {
				case err := <-providerResult:
					if trust == "valid" && err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal("provider did not join")
				}
				select {
				case <-done:
				case <-ctx.Done():
					t.Fatal("tunnel did not join")
				}
			})
		}
	}
}

func mailCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"mail.example.test"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, roots
}
