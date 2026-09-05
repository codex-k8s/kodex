package mailtransport

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
)

func readbackFixture() (Tunnel, http.Header) {
	tunnel := Tunnel{ConfigurationRevision: 7, ConfigurationDigest: strings.Repeat("b", 64), PolicyDigest: strings.Repeat("a", 64)}
	headers := http.Header{}
	for key, value := range map[string]string{
		"X-Kodex-Egress-Revision":               "mail-7",
		"X-Kodex-Egress-Digest":                 tunnel.PolicyDigest,
		"X-Kodex-Egress-Profile":                "email-mail",
		"X-Kodex-Egress-Workload":               "email-bridge",
		"X-Kodex-Egress-Operation":              "email.transport",
		"X-Kodex-Egress-Configuration-Revision": "7",
		"X-Kodex-Egress-Configuration-Digest":   tunnel.ConfigurationDigest,
	} {
		headers.Set(key, value)
	}
	return tunnel, headers
}

func TestMailReadbackRejectsBeforeProviderBytes(t *testing.T) {
	_, expected := readbackFixture()
	for key := range expected {
		for _, mode := range []string{"missing", "different", "duplicate"} {
			for _, tlsMode := range []string{"implicit", "starttls"} {
				t.Run(key+"/"+mode+"/"+tlsMode, func(t *testing.T) {
					tunnel, headers := readbackFixture()
					switch mode {
					case "missing":
						headers.Del(key)
					case "different":
						headers.Set(key, "foreign-generation")
					case "duplicate":
						headers.Add(key, headers.Get(key))
					}
					listener, err := net.Listen("tcp", "127.0.0.1:0")
					if err != nil {
						t.Fatal(err)
					}
					defer listener.Close()
					tunnel.Address = listener.Addr().String()
					done := make(chan error, 1)
					go func() {
						c, err := listener.Accept()
						if err != nil {
							done <- err
							return
						}
						defer c.Close()
						_ = c.SetDeadline(time.Now().Add(2 * time.Second))
						r := bufio.NewReader(c)
						request, err := http.ReadRequest(r)
						if err != nil {
							done <- err
							return
						}
						if request.Method != "CONNECT" || request.Host != "mail.example.test:993" || len(request.Header) != 0 {
							done <- fmt.Errorf("caller supplied authority or non-CONNECT request")
							return
						}
						_, _ = io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n")
						_ = headers.Write(c)
						_, _ = io.WriteString(c, "\r\n")
						b := make([]byte, 1)
						n, err := r.Read(b)
						if n != 0 || err != io.EOF {
							done <- fmt.Errorf("provider bytes or open socket after invalid readback")
							return
						}
						done <- nil
					}()
					provider := &Provider{Dialer: tunnel}
					connection, cleanup, err := provider.connect(t.Context(), api.Endpoint{Host: "mail.example.test", Port: 993, TlsMode: api.EndpointTlsMode(tlsMode)}, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "mail.example.test"}, 1024)
					if cleanup != nil {
						cleanup()
					}
					if err == nil || connection != nil {
						t.Error("invalid generation reached provider")
					}
					if err := <-done; err != nil {
						t.Fatal(err)
					}
				})
			}
		}
	}
}

func TestMailReadbackPreservesOpaqueGreetingAndBounds(t *testing.T) {
	for _, scenario := range []string{"greeting", "oversize", "malformed", "body", "old-source", "old-policy"} {
		t.Run(scenario, func(t *testing.T) {
			tunnel, headers := readbackFixture()
			if scenario == "old-source" {
				tunnel.ConfigurationRevision++
			}
			if scenario == "old-policy" {
				tunnel.PolicyDigest = strings.Repeat("c", 64)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			tunnel.Address = listener.Addr().String()
			done := make(chan struct{})
			go func() {
				defer close(done)
				c, e := listener.Accept()
				if e != nil {
					return
				}
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(time.Second))
				_, _ = http.ReadRequest(bufio.NewReader(c))
				var response strings.Builder
				response.WriteString("HTTP/1.1 200 Connection Established\r\n")
				_ = headers.Write(&response)
				if scenario == "oversize" {
					response.WriteString("X-Fill: " + strings.Repeat("x", 16384) + "\r\n")
				}
				if scenario == "malformed" {
					response.WriteString("invalid header\r\n")
				}
				if scenario == "body" {
					response.WriteString("Content-Length: 5\r\n")
				}
				response.WriteString("\r\n+OK server greeting\r\n")
				_, _ = io.WriteString(c, response.String())
			}()
			c, err := tunnel.Dial(t.Context(), "mail.example.test:110")
			if scenario == "greeting" {
				if err != nil {
					t.Fatal(err)
				}
				line, e := bufio.NewReader(c).ReadString('\n')
				if e != nil || line != "+OK server greeting\r\n" {
					t.Error("opaque greeting lost")
				}
			} else if err == nil {
				t.Error("unsafe CONNECT accepted")
			}
			if c != nil {
				c.Close()
			}
			<-done
		})
	}
}
