package component

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/mailtransport"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/service/mail"
	repository "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/repository/postgres/receipt"
	httptransport "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/transport/http"
	"github.com/emersion/go-sasl"
)

type providerFixture struct {
	oauthAuth                atomic.Int32
	mu                       sync.Mutex
	raw                      string
	uidlLines, listLines     string
	retrievals               atomic.Int32
	sent                     [][]byte
	deletes                  int
	dropSMTP, dropPOP, stall bool
	rejectUpgrade            atomic.Bool
	insecureAuth             atomic.Int32
	cert                     tls.Certificate
	ca                       []byte
	smtp, pop                string
	wg                       sync.WaitGroup
}

func certificate(t *testing.T) (tls.Certificate, []byte) {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	uri, _ := url.Parse(httptransport.CallerSPIFFE)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fixture"}, DNSNames: []string{"mail.example.test", "localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, URIs: []*url.URL{uri}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	k, _ := x509.MarshalPKCS8PrivateKey(key)
	cert, e := tls.X509KeyPair(ca, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: k}))
	if e != nil {
		t.Fatal(e)
	}
	return cert, ca
}
func newFixture(t *testing.T, mode string) *providerFixture {
	t.Helper()
	f := &providerFixture{}
	f.cert, f.ca = certificate(t)
	f.raw = "From: Sender <recipient@example.test>\r\nTo: sender@example.test\r\nCc: copy@example.test\r\nSubject: Fixture search\r\nMessage-ID: <source@example.test>\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=fixture\r\n\r\n--fixture\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nOriginal body\r\n--fixture\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=note.txt\r\nContent-Transfer-Encoding: base64\r\n\r\nYXR0YWNobWVudA==\r\n--fixture--\r\n"
	var listeners []net.Listener
	t.Cleanup(func() {
		for _, listener := range listeners {
			listener.Close()
		}
		f.wg.Wait()
	})
	for _, protocol := range []string{"smtp", "pop"} {
		listener, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		if protocol == "smtp" {
			f.smtp = listener.Addr().String()
		} else {
			f.pop = listener.Addr().String()
		}
		listeners = append(listeners, listener)
		f.wg.Add(1)
		go func(protocol string) {
			defer f.wg.Done()
			for {
				c, e := listener.Accept()
				if e != nil {
					return
				}
				f.wg.Add(1)
				go func() {
					defer f.wg.Done()
					defer c.Close()
					_ = c.SetDeadline(time.Now().Add(5 * time.Second))
					if mode == "implicit" {
						c = tls.Server(c, &tls.Config{Certificates: []tls.Certificate{f.cert}, MinVersion: tls.VersionTLS12})
					}
					if protocol == "smtp" {
						f.serveSMTP(c)
					} else {
						f.servePOP(c)
					}
				}()
			}
		}(protocol)
	}
	return f
}
func (f *providerFixture) serveSMTP(c net.Conn) {
	tp := textproto.NewConn(c)
	_ = tp.PrintfLine("220 fixture ESMTP")
	for {
		line, e := tp.ReadLine()
		if e != nil {
			return
		}
		switch strings.ToUpper(strings.Fields(line)[0]) {
		case "EHLO":
			_ = tp.PrintfLine("250-fixture\r\n250-STARTTLS\r\n250 AUTH PLAIN OAUTHBEARER")
		case "STARTTLS":
			if f.rejectUpgrade.Load() {
				_ = tp.PrintfLine("454 TLS unavailable")
				continue
			}
			_ = tp.PrintfLine("220 Ready")
			c = tls.Server(c, &tls.Config{Certificates: []tls.Certificate{f.cert}, MinVersion: tls.VersionTLS12})
			tp = textproto.NewConn(c)
		case "AUTH":
			fields := strings.Fields(line)
			if len(fields) > 1 && fields[1] == "OAUTHBEARER" {
				if len(fields) != 3 {
					_ = tp.PrintfLine("535 Authentication failed")
					continue
				}
				raw, err := base64.StdEncoding.DecodeString(fields[2])
				if err != nil {
					_ = tp.PrintfLine("535 Authentication failed")
					continue
				}
				valid := false
				auth := sasl.NewOAuthBearerServer(func(o sasl.OAuthBearerOptions) *sasl.OAuthBearerError {
					valid = o.Username == "fixture-value" && o.Token == "fixture-value"
					if !valid {
						return &sasl.OAuthBearerError{Status: "invalid_token"}
					}
					return nil
				})
				if _, done, err := auth.Next(raw); err != nil || !done || !valid {
					_ = tp.PrintfLine("535 Authentication failed")
					continue
				}
				f.oauthAuth.Add(1)
			}
			if _, secured := c.(*tls.Conn); !secured {
				f.insecureAuth.Add(1)
			}
			_ = tp.PrintfLine("235 Authenticated")
		case "MAIL", "RCPT", "RSET", "NOOP":
			_ = tp.PrintfLine("250 OK")
		case "DATA":
			_ = tp.PrintfLine("354 Send data")
			raw, e := tp.ReadDotBytes()
			if e != nil {
				return
			}
			f.mu.Lock()
			f.sent = append(f.sent, raw)
			drop, stall := f.dropSMTP, f.stall
			f.mu.Unlock()
			if drop {
				return
			}
			if stall {
				time.Sleep(1500 * time.Millisecond)
				return
			}
			_ = tp.PrintfLine("250 Accepted")
		case "QUIT":
			_ = tp.PrintfLine("221 Bye")
			return
		default:
			_ = tp.PrintfLine("500 Unsupported")
		}
	}
}
func (f *providerFixture) servePOP(c net.Conn) {
	tp := textproto.NewConn(c)
	_ = tp.PrintfLine("+OK fixture")
	deleted := false
	for {
		line, e := tp.ReadLine()
		if e != nil {
			return
		}
		parts := strings.Fields(line)
		if len(parts) == 0 {
			return
		}
		f.mu.Lock()
		gone := f.deletes > 0
		raw, uidlLines, listLines := f.raw, f.uidlLines, f.listLines
		f.mu.Unlock()
		switch parts[0] {
		case "STLS":
			if f.rejectUpgrade.Load() {
				_ = tp.PrintfLine("-ERR TLS unavailable")
				continue
			}
			_ = tp.PrintfLine("+OK TLS")
			c = tls.Server(c, &tls.Config{Certificates: []tls.Certificate{f.cert}, MinVersion: tls.VersionTLS12})
			tp = textproto.NewConn(c)
		case "USER", "PASS", "NOOP":
			if parts[0] != "NOOP" {
				if _, secured := c.(*tls.Conn); !secured {
					f.insecureAuth.Add(1)
				}
			}
			_ = tp.PrintfLine("+OK")
		case "UIDL":
			_ = tp.PrintfLine("+OK")
			w := tp.DotWriter()
			if uidlLines != "" {
				fmt.Fprint(w, uidlLines)
			} else if !gone {
				fmt.Fprintln(w, "1 uid-one\r\n2 uid-two")
			}
			w.Close()
		case "LIST":
			_ = tp.PrintfLine("+OK")
			w := tp.DotWriter()
			if listLines != "" {
				fmt.Fprint(w, listLines)
			} else if !gone {
				fmt.Fprintf(w, "1 %d\r\n2 %d\r\n", len(raw), len(raw))
			}
			w.Close()
		case "TOP", "RETR":
			if parts[0] == "RETR" {
				f.retrievals.Add(1)
			}
			_ = tp.PrintfLine("+OK")
			w := tp.DotWriter()
			value := raw
			if parts[0] == "TOP" {
				value = strings.Split(raw, "\r\n\r\n")[0] + "\r\n\r\n"
			}
			io.WriteString(w, value)
			w.Close()
		case "DELE":
			deleted = true
			_ = tp.PrintfLine("+OK")
		case "QUIT":
			f.mu.Lock()
			if deleted {
				f.deletes++
			}
			drop := f.dropPOP
			f.mu.Unlock()
			if drop {
				return
			}
			_ = tp.PrintfLine("+OK")
			return
		default:
			_ = tp.PrintfLine("-ERR")
		}
	}
}

type dialFixture struct{ smtp, pop string }

func (d dialFixture) Dial(ctx context.Context, target string) (net.Conn, error) {
	address := d.pop
	if strings.HasSuffix(target, ":465") || strings.HasSuffix(target, ":587") {
		address = d.smtp
	}
	return (&net.Dialer{}).DialContext(ctx, "tcp", address)
}

type secrets struct {
	ca      []byte
	reads   atomic.Int32
	revoked atomic.Bool
}

func (s *secrets) Read(_ context.Context, d api.Descriptor) ([]byte, error) {
	s.reads.Add(1)
	if s.revoked.Load() {
		return nil, errs.Unavailable
	}
	if d.Name == "ca" {
		return s.ca, nil
	}
	return []byte("fixture-value"), nil
}

type authorityFixture struct {
	mutate  func(*api.AuthorizationDecision)
	revoked bool
}

func (a *authorityFixture) Resolve(_ context.Context, r api.AuthorizationRequest) (api.AuthorizationDecision, error) {
	if a.revoked {
		return api.AuthorizationDecision{}, errs.Denied
	}
	scope := api.Scope{Folders: []string{"INBOX"}, MailboxId: r.MailboxId, Sender: r.Sender, Operations: []api.Operation{r.Operation}, Recipients: []string{"recipient@example.test", "copy@example.test"}}
	d := api.AuthorizationDecision{Allowed: true, ActorId: "actor", AgentId: "agent", TenantId: "tenant", ConnectionId: "connection", MailboxId: r.MailboxId, Operation: r.Operation, InputSha256: r.InputSha256, EffectKey: r.EffectKey, ConfigurationRevision: r.ConfigurationRevision, CredentialGeneration: 1, GrantId: "grant", ExpiresAt: time.Now().Add(time.Minute).Unix(), Policy: api.Allow, GateApproved: true, UserScope: scope, AgentScope: scope, ConnectionScope: scope, ResourceScope: scope}
	if a.mutate != nil {
		a.mutate(&d)
	}
	d.ExecutionBinding = r.ExecutionBinding
	return d, nil
}

type memory struct {
	mu   sync.Mutex
	rows map[string]port.Record
}

func (m *memory) Reserve(_ context.Context, s port.Scope, key, digest, id, resource string, audit port.Audit) (port.Record, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := s.Tenant + "/" + s.Mailbox + "/" + key
	if r, ok := m.rows[k]; ok {
		if r.Digest != digest {
			return r, false, errs.Conflict
		}
		return r, false, nil
	}
	for rowKey, previous := range m.rows {
		if resource != "" && previous.Resource == resource && previous.Status == "unknown" && strings.HasPrefix(rowKey, s.Tenant+"/"+s.Mailbox+"/") {
			return port.Record{}, false, errs.Conflict
		}
	}
	r := port.Record{ID: id, Key: key, Digest: digest, Status: "unknown", Resource: resource, Audit: audit}
	m.rows[k] = r
	return r, true, nil
}
func (m *memory) Complete(_ context.Context, s port.Scope, r port.Record, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r.Status = status
	m.rows[s.Tenant+"/"+s.Mailbox+"/"+r.Key] = r
	return nil
}
func (m *memory) Get(_ context.Context, s port.Scope, id, key string) (port.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, r := range m.rows {
		if strings.HasPrefix(k, s.Tenant+"/"+s.Mailbox+"/") && (r.ID == id || r.Key == key) {
			return r, nil
		}
	}
	return port.Record{}, errs.NotFound
}
func (m *memory) Configuration(context.Context, api.Configuration, string) error { return nil }
func (m *memory) Ready(context.Context) error                                    { return nil }
func configuration(mode string) api.Configuration {
	d := func(name string) api.Descriptor { return api.Descriptor{Name: name, Generation: 1} }
	endpoint := api.Endpoint{Host: "mail.example.test", ServerName: "mail.example.test", Port: 465, TlsMode: api.EndpointTlsMode(mode), Ca: d("ca"), Username: d("user"), AuthMethod: "password", Secret: d("password")}
	box := api.Mailbox{Id: "mailbox", TenantId: "tenant", ConnectionId: "connection", Revision: 1, CredentialGeneration: 1, Enabled: true, ReceiveProtocol: "pop3", AllowedFolders: []string{"INBOX"}, ReplyTo: "sender@example.test", Folder: "INBOX", Sender: "sender@example.test", EnvelopeFrom: "sender@example.test", HelloName: "bridge.example.test", Recipients: []string{"recipient@example.test", "copy@example.test"}, Smtp: endpoint, Pop: &endpoint, Limits: api.Limits{TimeoutSeconds: 2, MessageBytes: 65536, AttachmentBytes: 32768, MaxAttachments: 5, MaxRecipients: 10, PageSize: 1, ScanMessages: 100}}
	box.Pop.Port = 995
	if mode == "starttls" {
		box.Smtp.Port = 587
		box.Pop.Port = 110
	}
	for _, op := range api.Operations() {
		box.Policies = append(box.Policies, api.OperationPolicy{Operation: op, Policy: api.Allow})
	}
	return api.Configuration{Version: "email-bridge/v1", Revision: 1, ManagedBy: "git", Source: "fixture", Mailboxes: []api.Mailbox{box}}
}

// Одна disposable БД использует один immutable configuration digest во всех tests.
func receiptConfiguration() api.Configuration {
	c := configuration("implicit")
	c.Mailboxes[0].ReceiveProtocol = "imap"
	endpoint := c.Mailboxes[0].Smtp
	endpoint.Port = 993
	c.Mailboxes[0].Imap = &endpoint
	return c
}
func service(t *testing.T, f *providerFixture, mode string, store port.Repository) (*mail.Service, *secrets, *authorityFixture) {
	t.Helper()
	sec := &secrets{ca: f.ca}
	auth := &authorityFixture{}
	if store == nil {
		store = &memory{rows: map[string]port.Record{}}
	}
	s := &mail.Service{CompletionBase: t.Context(), Config: configuration(mode), Authority: auth, Effects: effectFixture{}, Provider: &mailtransport.Provider{Secrets: sec, Dialer: dialFixture{f.smtp, f.pop}}, Receipts: store}
	return s, sec, auth
}
func send(op api.Operation, key string) api.Command {
	return api.Command{Operation: op, MailboxId: "mailbox", EffectKey: key, Message: api.MessageInput{From: "sender@example.test", To: "recipient@example.test", Subject: "Fixture", BodyText: "Message body", Attachments: []api.Attachment{{Filename: "hello.txt", ContentType: "text/plain", ContentBase64: base64.StdEncoding.EncodeToString([]byte("hello"))}}}}
}
func execute(t *testing.T, s *mail.Service, c api.Command) api.Result {
	t.Helper()
	r, e := s.Execute(t.Context(), httptransport.CallerSPIFFE, "fixture-token", c)
	if e != nil {
		t.Fatalf("operation %s: %v", c.Operation, e)
	}
	return r
}

func TestProtocolOperations(t *testing.T) {
	for _, mode := range []string{"implicit", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, mode)
			s, _, _ := service(t, f, mode, nil)
			if e := api.ValidateConfiguration(s.Config); e != nil {
				t.Fatal(e)
			}
			if execute(t, s, api.Command{Operation: api.OperationHealth, MailboxId: "mailbox"}).Status != "ready" {
				t.Fatal("not ready")
			}
			if len(execute(t, s, api.Command{Operation: api.OperationMailboxes, MailboxId: "mailbox"}).Mailboxes) != 1 {
				t.Fatal("discovery")
			}
			list := execute(t, s, api.Command{Operation: api.OperationList, MailboxId: "mailbox"})
			if len(list.Headers) != 1 || list.NextCursor == "" {
				t.Fatal("pagination")
			}
			page := execute(t, s, api.Command{Operation: api.OperationList, MailboxId: "mailbox", Cursor: list.NextCursor})
			if len(page.Headers) != 1 || page.NextCursor != "" {
				t.Fatal("last page")
			}
			if len(execute(t, s, api.Command{Operation: api.OperationSearch, MailboxId: "mailbox", Query: "search"}).Headers) != 1 {
				t.Fatal("search")
			}
			fetched := execute(t, s, api.Command{Operation: api.OperationFetch, MailboxId: "mailbox", Uid: "uid-one"})
			if !strings.Contains(fetched.BodyText, "Original body") || len(fetched.Attachments) != 1 {
				t.Fatal("fetch MIME")
			}
			if len(execute(t, s, api.Command{Operation: api.OperationDownload, MailboxId: "mailbox", Uid: "uid-one"}).Attachments) != 1 {
				t.Fatal("download")
			}
			if _, e := s.Execute(t.Context(), httptransport.CallerSPIFFE, "token", api.Command{Operation: api.OperationMark, MailboxId: "mailbox"}); !errors.Is(e, errs.Unsupported) {
				t.Fatal("POP flags invented")
			}
			for _, op := range []api.Operation{api.OperationSend, api.OperationReply, api.OperationReplyAll, api.OperationForward} {
				cmd := send(op, string(op))
				if op != api.OperationSend {
					cmd.Message.SourceUid = "uid-one"
				}
				if op == api.OperationReplyAll {
					cmd.Message.Cc = []string{"copy@example.test"}
				}
				result := execute(t, s, cmd)
				if result.Status != "accepted" {
					t.Fatalf("%s: %s", op, result.Status)
				}
				duplicate := execute(t, s, cmd)
				if duplicate.MessageId != result.MessageId {
					t.Fatal("receipt differs")
				}
				receipt := execute(t, s, api.Command{Operation: api.OperationReceipt, MailboxId: "mailbox", EffectKey: cmd.EffectKey})
				if receipt.Status != "accepted" {
					t.Fatal("receipt read")
				}
			}
			f.mu.Lock()
			if len(f.sent) != 4 {
				t.Errorf("effects=%d", len(f.sent))
			}
			if !bytes.Contains(f.sent[1], []byte("In-Reply-To:")) || !bytes.Contains(f.sent[3], []byte("forwarded.eml")) {
				t.Error("reply/forward MIME")
			}
			f.mu.Unlock()
			if execute(t, s, api.Command{Operation: api.OperationDelete, MailboxId: "mailbox", Uid: "uid-one", EffectKey: "delete"}).Status != "deleted" {
				t.Fatal("delete")
			}
			if _, e := s.Execute(t.Context(), httptransport.CallerSPIFFE, "token", api.Command{Operation: api.OperationList, MailboxId: "mailbox", Cursor: list.NextCursor}); !errors.Is(e, errs.Conflict) {
				t.Fatal("stale cursor")
			}
		})
	}
}

func TestAuthorityBeforeCredentials(t *testing.T) {
	f := newFixture(t, "implicit")
	for _, name := range []string{"tenant", "user", "agent", "connection", "resource", "gate", "revocation", "generation", "digest", "expiry", "deny"} {
		t.Run(name, func(t *testing.T) {
			s, sec, a := service(t, f, "implicit", nil)
			a.mutate = func(d *api.AuthorizationDecision) {
				switch name {
				case "tenant":
					d.TenantId = "foreign"
				case "user":
					d.UserScope.Operations = nil
				case "agent":
					d.AgentScope.Recipients = nil
				case "connection":
					d.ConnectionScope.MailboxId = "foreign"
				case "resource":
					d.ResourceScope.Recipients = nil
				case "gate":
					d.Policy = api.HumanGate
					d.GateApproved = false
				case "generation":
					d.CredentialGeneration = 2
				case "digest":
					d.InputSha256 = "wrong"
				case "expiry":
					d.ExpiresAt = 1
				case "deny":
					d.Policy = api.Deny
				}
			}
			a.revoked = name == "revocation"
			_, e := s.Execute(t.Context(), httptransport.CallerSPIFFE, "token", send(api.OperationSend, name))
			if e == nil || sec.reads.Load() != 0 {
				t.Fatalf("denial before projection: err=%v reads=%d", e, sec.reads.Load())
			}
		})
	}
	s, sec, _ := service(t, f, "implicit", nil)
	cmd := send(api.OperationSend, "scope")
	cmd.Message.To = "foreign@example.test"
	if _, e := s.Execute(t.Context(), httptransport.CallerSPIFFE, "token", cmd); e == nil || sec.reads.Load() != 0 {
		t.Fatal("recipient scope")
	}
	sec.revoked.Store(true)
	result := execute(t, s, send(api.OperationSend, "credential-revoked"))
	if result.Status != "unknown" {
		t.Fatal("revoked credential effect")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) != 0 {
		t.Fatal("unexpected provider effects")
	}
}

func TestUnknownTimeoutTLSAndDuplicate(t *testing.T) {
	for _, mode := range []string{"drop", "timeout", "hostname", "ca", "pop-drop"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, "implicit")
			s, sec, _ := service(t, f, "implicit", nil)
			f.dropSMTP = mode == "drop"
			f.stall = mode == "timeout"
			f.dropPOP = mode == "pop-drop"
			s.Config.Mailboxes[0].Limits.TimeoutSeconds = 1
			if mode == "hostname" {
				s.Config.Mailboxes[0].Smtp.ServerName = "foreign.example.test"
			}
			if mode == "ca" {
				_, sec.ca = certificate(t)
			}
			cmd := send(api.OperationSend, mode)
			if mode == "pop-drop" {
				cmd = api.Command{Operation: api.OperationDelete, MailboxId: "mailbox", Uid: "uid-one", EffectKey: mode}
			}
			first := execute(t, s, cmd)
			if first.Status != "unknown" {
				t.Fatalf("state=%s", first.Status)
			}
			replacement, _, _ := service(t, f, "implicit", s.Receipts)
			replacement.Config = s.Config
			if execute(t, replacement, cmd).MessageId != first.MessageId {
				t.Fatal("restart replay")
			}
			cmd.Message.Subject = "changed"
			if _, e := s.Execute(t.Context(), httptransport.CallerSPIFFE, "token", cmd); !errors.Is(e, errs.Conflict) {
				t.Fatal("input mismatch")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.sent) > 1 || f.deletes > 1 {
				t.Fatal("repeated effect")
			}
		})
	}
}

func TestHTTPSBoundary(t *testing.T) {
	f := newFixture(t, "implicit")
	s, _, _ := service(t, f, "implicit", nil)
	server := httptest.NewUnstartedServer(httptransport.Handler{Service: s})
	ca := x509.NewCertPool()
	ca.AppendCertsFromPEM(f.ca)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{f.cert}, ClientCAs: ca, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	defer server.Close()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: ca, Certificates: []tls.Certificate{f.cert}, MinVersion: tls.VersionTLS12}}}
	defer client.CloseIdleConnections()
	raw, _ := json.Marshal(send(api.OperationSend, "https").Message)
	request, _ := http.NewRequestWithContext(t.Context(), "POST", server.URL+"/v1/messages?sender=sender%40example.test", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	binding := executionFixture()
	header, _ := api.ExecutionHeaderValue(binding)
	request.Header.Set("Authorization", "Bearer "+binding.Lease.Fence)
	request.Header.Set(api.ExecutionHeader, header)
	request.Header.Set("Idempotency-Key", "https")
	response, e := client.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	if response.StatusCode != 202 {
		t.Fatalf("status=%d", response.StatusCode)
	}
	var status api.MessageStatus
	if json.NewDecoder(response.Body).Decode(&status) != nil || status.Status != api.Accepted {
		t.Fatal("typed legacy receipt")
	}
	request, _ = http.NewRequestWithContext(t.Context(), "GET", server.URL+"/v1/health?sender=sender%40example.test", nil)
	response, e = client.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("missing bearer accepted")
	}
}

func TestPostgresEffects(t *testing.T) {
	store := postgresFixture(t)
	pool := store.Pool
	var e error
	f := newFixture(t, "implicit")
	s, _, _ := service(t, f, "implicit", store)
	s.Config = receiptConfiguration()
	cmd := send(api.OperationSend, "concurrent")
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.Execute(t.Context(), httptransport.CallerSPIFFE, "token", cmd)
			if e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	f.mu.Lock()
	if len(f.sent) != 1 {
		t.Errorf("SMTP effects=%d", len(f.sent))
	}
	f.dropSMTP = true
	f.mu.Unlock()
	r := execute(t, s, send(api.OperationSend, "crash"))
	if r.Status != "unknown" {
		t.Fatal("unknown")
	}
	replacement, _, _ := service(t, f, "implicit", &repository.Repository{Pool: pool})
	replacement.Config = receiptConfiguration()
	if execute(t, replacement, send(api.OperationSend, "crash")).MessageId != r.MessageId {
		t.Fatal("durable unknown")
	}
	if _, e = store.Get(t.Context(), port.Scope{Tenant: "foreign", Mailbox: "mailbox"}, r.MessageId, ""); !errors.Is(e, errs.NotFound) {
		t.Fatal("cross tenant receipt")
	}
	scope := port.Scope{Tenant: "tenant", Mailbox: "mailbox"}
	audit := port.Audit{Actor: "actor", Agent: "agent", Grant: "grant", Operation: api.OperationDraftUpdate, ConfigurationRevision: 1, CredentialGeneration: 1, GateApproved: true}
	reserved, created, e := store.Reserve(t.Context(), scope, "imap-receipt", strings.Repeat("a", 64), "imap-receipt-id", strings.Repeat("b", 64), audit)
	if e != nil || !created {
		t.Fatal("IMAP resource reservation")
	}
	if _, _, e = store.Reserve(t.Context(), scope, "imap-other", strings.Repeat("c", 64), "imap-other-id", strings.Repeat("b", 64), audit); !errors.Is(e, errs.Conflict) {
		t.Fatal("unknown source admitted another effect")
	}
	reserved.UID = "7"
	reserved.UIDValidity = 4294967295
	reserved.Folder = "Drafts"
	reserved.ContentDigest = strings.Repeat("d", 64)
	if e = store.Complete(t.Context(), scope, reserved, "unknown"); e != nil {
		t.Fatal("partial metadata persistence")
	}
	persisted, e := (&repository.Repository{Pool: pool}).Get(t.Context(), scope, reserved.ID, "")
	if e != nil || persisted.UID != reserved.UID || persisted.UIDValidity != reserved.UIDValidity || persisted.Folder != reserved.Folder || persisted.ContentDigest != reserved.ContentDigest || persisted.Status != "unknown" || persisted.Audit != audit {
		t.Fatal("durable IMAP partial metadata")
	}
	if e = store.Complete(t.Context(), scope, persisted, "accepted"); e != nil {
		t.Fatal("terminal metadata persistence")
	}
	if _, created, e = store.Reserve(t.Context(), scope, "imap-other", strings.Repeat("c", 64), "imap-other-id", strings.Repeat("b", 64), audit); e != nil || !created {
		t.Fatal("known outcome did not release resource")
	}
	c := s.Config
	c.Revision = 2
	if e = store.Configuration(t.Context(), c, api.Digest(c)); e != nil {
		t.Fatal(e)
	}
	if e = store.Configuration(t.Context(), s.Config, api.Digest(s.Config)); e == nil {
		t.Fatal("rollback accepted")
	}
}

func TestProjectedCredentialRevocation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "credential", "1")
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte("fixture"), 0400); e != nil {
		t.Fatal(e)
	}
	files := mailtransport.Files{Root: root}
	d := api.Descriptor{Name: "credential", Generation: 1}
	if _, e := files.Read(t.Context(), d); e != nil {
		t.Fatal(e)
	}
	if e := os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if _, e := files.Read(t.Context(), d); e == nil {
		t.Fatal("removed credential remained available")
	}
	d.Name = "../escape"
	if _, e := files.Read(t.Context(), d); e == nil {
		t.Fatal("descriptor traversal")
	}
}

func TestCONNECTTransport(t *testing.T) {
	f := newFixture(t, "starttls")
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	t.Cleanup(func() { listener.Close(); wg.Wait() })
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			connection, e := listener.Accept()
			if e != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer connection.Close()
				_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
				reader := bufio.NewReader(connection)
				request, e := http.ReadRequest(reader)
				if e != nil || request.Method != "CONNECT" {
					return
				}
				address := f.pop
				if request.Host == "mail.example.test:587" {
					address = f.smtp
				} else if request.Host != "mail.example.test:110" {
					io.WriteString(connection, "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n")
					return
				}
				upstream, e := net.DialTimeout("tcp", address, time.Second)
				if e != nil {
					return
				}
				defer upstream.Close()
				io.WriteString(connection, "HTTP/1.1 200 Connection Established\r\n\r\n")
				done := make(chan struct{})
				go func() { io.Copy(upstream, reader); upstream.Close(); close(done) }()
				io.Copy(connection, upstream)
				connection.Close()
				<-done
			}()
		}
	}()
	s, _, _ := service(t, f, "starttls", nil)
	s.Provider = &mailtransport.Provider{Secrets: &secrets{ca: f.ca}, Dialer: mailtransport.Tunnel{Address: listener.Addr().String()}}
	if execute(t, s, send(api.OperationSend, "connect")).Status != "accepted" {
		t.Fatal("STARTTLS through CONNECT")
	}
	if execute(t, s, api.Command{Operation: api.OperationFetch, MailboxId: "mailbox", Uid: "uid-one"}).Status != "ok" {
		t.Fatal("POP STLS through CONNECT")
	}
	if c, e := (mailtransport.Tunnel{Address: listener.Addr().String()}).Dial(t.Context(), "foreign.example.test:995"); e == nil {
		c.Close()
		t.Fatal("proxy denial bypassed")
	}
}
