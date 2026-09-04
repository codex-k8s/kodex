package mailtransport

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
)

type Secrets interface {
	Read(context.Context, api.Descriptor) ([]byte, error)
}
type Files struct{ Root string }

func (f Files) Read(ctx context.Context, d api.Descriptor) ([]byte, error) {
	if ctx.Err() != nil || !api.DescriptorValid(d) {
		return nil, errs.Unavailable
	}
	b, e := securefile.ReadWithin(f.Root, filepath.Join(f.Root, d.Name, strconv.FormatInt(d.Generation, 10)), 1<<20)
	if e != nil {
		return nil, errs.Unavailable
	}
	return b, nil
}

type Dialer interface {
	Dial(context.Context, string) (net.Conn, error)
}

// Tunnel не содержит прямого внешнего dial: только platform egress CONNECT.
type Tunnel struct{ Address string }

func (t Tunnel) Dial(ctx context.Context, target string) (net.Conn, error) {
	c, e := (&net.Dialer{}).DialContext(ctx, "tcp", t.Address)
	if e != nil {
		return nil, errs.Unavailable
	}
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = c.SetDeadline(deadline)
	if _, e = fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target); e != nil {
		c.Close()
		return nil, errs.Unavailable
	}
	reader := bufio.NewReader(io.LimitReader(c, 16384))
	resp, e := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if e != nil || resp.StatusCode != 200 {
		c.Close()
		return nil, errs.Unavailable
	}
	if reader.Buffered() > 0 {
		buffered, _ := reader.Peek(reader.Buffered())
		return &greetingConn{Conn: c, reader: io.MultiReader(bytes.NewReader(bytes.Clone(buffered)), c)}, nil
	}
	return c, nil
}

type Provider struct {
	Secrets Secrets
	Dialer  Dialer
}

func (p *Provider) material(ctx context.Context, e api.Endpoint) (*tls.Config, string, string, error) {
	ca, err := p.Secrets.Read(ctx, e.Ca)
	if err != nil {
		return nil, "", "", errs.Unavailable
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, "", "", errs.Unavailable
	}
	u, err := p.Secrets.Read(ctx, e.Username)
	if err != nil {
		return nil, "", "", errs.Unavailable
	}
	pw, err := p.Secrets.Read(ctx, e.Password)
	if err != nil {
		return nil, "", "", errs.Unavailable
	}
	if len(u) == 0 || len(u) > 320 || len(pw) == 0 || len(pw) > 4096 || strings.ContainsAny(string(u)+string(pw), "\r\n\x00") {
		return nil, "", "", errs.Unavailable
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, ServerName: e.ServerName, RootCAs: roots}, string(u), string(pw), nil
}

// cappedConn ограничивает фактический объём до выделения памяти библиотекой.
type cappedConn struct {
	net.Conn
	remaining int64
	deadline  time.Time
}

func (c *cappedConn) Read(b []byte) (int, error) {
	if c.remaining <= 0 {
		return 0, errs.Unavailable
	}
	if int64(len(b)) > c.remaining {
		b = b[:c.remaining]
	}
	n, e := c.Conn.Read(b)
	c.remaining -= int64(n)
	return n, e
}
func (c *cappedConn) SetDeadline(d time.Time) error {
	if d.IsZero() || d.After(c.deadline) {
		d = c.deadline
	}
	return c.Conn.SetDeadline(d)
}
func (c *cappedConn) SetReadDeadline(d time.Time) error {
	if d.IsZero() || d.After(c.deadline) {
		d = c.deadline
	}
	return c.Conn.SetReadDeadline(d)
}
func (c *cappedConn) SetWriteDeadline(d time.Time) error {
	if d.IsZero() || d.After(c.deadline) {
		d = c.deadline
	}
	return c.Conn.SetWriteDeadline(d)
}
func (p *Provider) connect(ctx context.Context, e api.Endpoint, config *tls.Config, maxBytes int) (net.Conn, func(), error) {
	c, err := p.Dialer.Dial(ctx, net.JoinHostPort(e.Host, strconv.Itoa(e.Port)))
	if err != nil {
		return nil, nil, errs.Unavailable
	}
	d, ok := ctx.Deadline()
	if !ok {
		d = time.Now().Add(time.Minute)
	}
	bounded := &cappedConn{Conn: c, remaining: int64(maxBytes), deadline: d}
	_ = bounded.SetDeadline(d)
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	cleanup := func() { stop(); _ = c.Close() }
	if e.TlsMode == "implicit" {
		secured := tls.Client(bounded, config)
		if secured.HandshakeContext(ctx) != nil {
			cleanup()
			return nil, nil, errs.Unavailable
		}
		return secured, cleanup, nil
	}
	return bounded, cleanup, nil
}

func popStartTLS(ctx context.Context, c net.Conn, config *tls.Config) (net.Conn, error) {
	tp := textproto.NewConn(c)
	line, e := tp.ReadLine()
	if e != nil || !strings.HasPrefix(line, "+OK") {
		return nil, errs.Unavailable
	}
	if tp.PrintfLine("STLS") != nil {
		return nil, errs.Unavailable
	}
	line, e = tp.ReadLine()
	if e != nil || !strings.HasPrefix(line, "+OK") {
		return nil, errs.Unavailable
	}
	secured := tls.Client(c, config)
	if secured.HandshakeContext(ctx) != nil {
		return nil, errs.Unavailable
	}
	return &greetingConn{Conn: secured, reader: io.MultiReader(strings.NewReader("+OK\r\n"), secured)}, nil
}

type greetingConn struct {
	net.Conn
	reader io.Reader
}

func (c *greetingConn) Read(b []byte) (int, error) { return c.reader.Read(b) }
