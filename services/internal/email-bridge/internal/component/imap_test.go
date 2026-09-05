package component

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/mailtransport"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"
)

type imapDialFixture struct {
	other   dialFixture
	address string
}

func (d imapDialFixture) Dial(ctx context.Context, target string) (net.Conn, error) {
	if strings.HasSuffix(target, ":993") || strings.HasSuffix(target, ":143") {
		return (&net.Dialer{}).DialContext(ctx, "tcp", d.address)
	}
	return d.other.Dial(ctx, target)
}

type literalFixture struct {
	*strings.Reader
	size int64
}

func (r literalFixture) Size() int64 { return r.size }

func imapFixture(t *testing.T, f *providerFixture, mode string, wrappers ...func(imapserver.Session, *imapserver.Conn) imapserver.Session) (*imapmemserver.User, string) {
	return imapFixtureCaps(t, f, mode, true, wrappers...)
}
func imapFixtureCaps(t *testing.T, f *providerFixture, mode string, nativeMove bool, wrappers ...func(imapserver.Session, *imapserver.Conn) imapserver.Session) (*imapmemserver.User, string) {
	t.Helper()
	backend := imapmemserver.New()
	user := imapmemserver.NewUser("fixture-value", "fixture-value")
	for _, folder := range []string{"INBOX", "Archive", "Drafts", "Private"} {
		if err := user.Create(folder, nil); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		raw := f.raw
		if _, err := user.Append("INBOX", literalFixture{strings.NewReader(raw), int64(len(raw))}, &imap.AppendOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	backend.AddUser(user)
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{f.cert}}
	caps := imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapUIDPlus: {}}
	if nativeMove {
		caps[imap.CapMove] = struct{}{}
	}
	server := imapserver.New(&imapserver.Options{NewSession: func(c *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		var session imapserver.Session = backend.NewSession()
		for _, wrap := range wrappers {
			session = wrap(session, c)
		}
		return session, nil, nil
	}, TLSConfig: tlsConfig, Caps: caps})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if mode == "implicit" {
		listener = tls.NewListener(listener, tlsConfig)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close(); <-done })
	return user, address
}

type lostAppendSession struct {
	imapserver.Session
	conn    *imapserver.Conn
	drop    *atomic.Bool
	appends *atomic.Int32
}

type oauthIMAPSession struct {
	imapserver.Session
	accepted *atomic.Int32
}

func (s *oauthIMAPSession) AuthenticateMechanisms() []string { return []string{sasl.OAuthBearer} }
func (s *oauthIMAPSession) Authenticate(mechanism string) (sasl.Server, error) {
	if mechanism != sasl.OAuthBearer {
		return nil, imapserver.ErrAuthFailed
	}
	return sasl.NewOAuthBearerServer(func(o sasl.OAuthBearerOptions) *sasl.OAuthBearerError {
		if o.Username != "fixture-value" || o.Token != "fixture-value" || s.Session.Login(o.Username, o.Token) != nil {
			return &sasl.OAuthBearerError{Status: "invalid_token"}
		}
		s.accepted.Add(1)
		return nil
	}), nil
}
func (s *oauthIMAPSession) Move(w *imapserver.MoveWriter, set imap.NumSet, folder string) error {
	return s.Session.(imapserver.SessionMove).Move(w, set, folder)
}

func TestIMAPSMTPAuthAndReadiness(t *testing.T) {
	for _, mode := range []string{"implicit", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, mode)
			var accepted atomic.Int32
			_, address := imapFixture(t, f, mode, func(s imapserver.Session, _ *imapserver.Conn) imapserver.Session {
				return &oauthIMAPSession{s, &accepted}
			})
			s, sec, _ := service(t, f, mode, nil)
			m := &s.Config.Mailboxes[0]
			m.Smtp.AuthMethod = api.Oauthbearer
			endpoint := m.Smtp
			endpoint.Port = 993
			if mode == "starttls" {
				endpoint.Port = 143
			}
			m.Imap = &endpoint
			m.ReceiveProtocol = "imap"
			s.Provider = &mailtransport.Provider{Secrets: sec, Dialer: imapDialFixture{dialFixture{f.smtp, f.pop}, address}}
			r := execute(t, s, api.Command{Operation: api.OperationHealth, MailboxId: "mailbox"})
			if r.Status != "ready" || r.ProtocolReadiness == nil || r.ProtocolReadiness.Imap != "ready" || r.ProtocolReadiness.Smtp != "ready" || r.ProtocolReadiness.Pop3 != "ready" || accepted.Load() != 1 || f.oauthAuth.Load() != 1 || f.insecureAuth.Load() != 0 {
				t.Fatal("OAuth readiness")
			}
			m.Pop = nil
			r = execute(t, s, api.Command{Operation: api.OperationHealth, MailboxId: "mailbox"})
			if r.Status != "ready" || r.ProtocolReadiness.Pop3 != "not_configured" {
				t.Fatal("optional POP readiness")
			}
			m.Imap.ServerName = "wrong.example.test"
			r = execute(t, s, api.Command{Operation: api.OperationHealth, MailboxId: "mailbox"})
			if r.Status != "not_ready" || r.ProtocolReadiness.Imap != "not_ready" || r.ProtocolReadiness.Smtp != "ready" || accepted.Load() != 2 {
				t.Fatal("IMAP hostname failure readiness")
			}
		})
	}
}
func (s *lostAppendSession) Append(folder string, r imap.LiteralReader, opts *imap.AppendOptions) (*imap.AppendData, error) {
	result, err := s.Session.Append(folder, r, opts)
	if err == nil {
		s.appends.Add(1)
		if s.drop.Load() {
			_ = s.conn.NetConn().Close()
		}
	}
	return result, err
}
func (s *lostAppendSession) Move(w *imapserver.MoveWriter, set imap.NumSet, folder string) error {
	return s.Session.(imapserver.SessionMove).Move(w, set, folder)
}

func TestIMAPUnknownAppendDoesNotRepeat(t *testing.T) {
	f := newFixture(t, "implicit")
	var drop atomic.Bool
	var appends atomic.Int32
	_, address := imapFixture(t, f, "implicit", func(session imapserver.Session, c *imapserver.Conn) imapserver.Session {
		return &lostAppendSession{session, c, &drop, &appends}
	})
	store := &memory{rows: map[string]port.Record{}}
	s, sec, auth := service(t, f, "implicit", store)
	m := &s.Config.Mailboxes[0]
	endpoint := m.Smtp
	endpoint.Port = 993
	m.Imap = &endpoint
	m.ReceiveProtocol = "imap"
	m.AllowedFolders = []string{"INBOX", "Drafts"}
	m.DraftsFolder = "Drafts"
	auth.mutate = func(d *api.AuthorizationDecision) {
		for _, scope := range []*api.Scope{&d.UserScope, &d.AgentScope, &d.ConnectionScope, &d.ResourceScope} {
			scope.Folders = []string{"INBOX", "Drafts"}
		}
	}
	s.Provider = &mailtransport.Provider{Secrets: sec, Dialer: imapDialFixture{dialFixture{f.smtp, f.pop}, address}}
	created := execute(t, s, send(api.OperationDraftCreate, "create"))
	if created.Status != "accepted" {
		t.Fatal("create")
	}
	drop.Store(true)
	cmd := send(api.OperationDraftUpdate, "update")
	cmd.Uid = created.Uid
	cmd.UidValidity = created.UidValidity
	cmd.ExpectedDigest = created.ContentDigest
	unknown := execute(t, s, cmd)
	if unknown.Status != "unknown" || appends.Load() != 2 {
		t.Fatal("lost APPEND not unknown")
	}
	reads := sec.reads.Load()
	restarted := *s
	if retry := execute(t, &restarted, cmd); retry.MessageId != unknown.MessageId || retry.Status != "unknown" {
		t.Fatal("restarted receipt")
	}
	cmd.EffectKey = "another-key"
	if _, err := restarted.Execute(t.Context(), "caller", "token", cmd); !errors.Is(err, errs.Conflict) {
		t.Fatal("ambiguous source re-executed with another key")
	}
	if appends.Load() != 2 || sec.reads.Load() != reads {
		t.Fatal("duplicate credentials or append")
	}
	if receipt := execute(t, &restarted, api.Command{Operation: api.OperationReceipt, MailboxId: "mailbox", EffectKey: "update"}); receipt.Status != "unknown" {
		t.Fatal("unknown receipt read")
	}
}

func TestIMAPReadOperations(t *testing.T) {
	for _, mode := range []string{"implicit", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, mode)
			user, address := imapFixture(t, f, mode)
			s, sec, auth := service(t, f, mode, nil)
			m := &s.Config.Mailboxes[0]
			endpoint := m.Smtp
			endpoint.Port = 993
			if mode == "starttls" {
				endpoint.Port = 143
			}
			m.Imap = &endpoint
			m.ReceiveProtocol = "imap"
			m.AllowedFolders = []string{"INBOX", "Archive", "Drafts"}
			m.ArchiveFolder = "Archive"
			m.DraftsFolder = "Drafts"
			auth.mutate = func(d *api.AuthorizationDecision) {
				for _, scope := range []*api.Scope{&d.UserScope, &d.AgentScope, &d.ConnectionScope, &d.ResourceScope} {
					scope.Folders = []string{"INBOX", "Archive", "Drafts"}
				}
			}
			s.Provider = &mailtransport.Provider{Secrets: sec, Dialer: imapDialFixture{dialFixture{f.smtp, f.pop}, address}}
			if err := api.ValidateConfiguration(s.Config); err != nil {
				t.Fatal(err)
			}
			if execute(t, s, api.Command{Operation: api.OperationHealth, MailboxId: "mailbox"}).Status != "ready" {
				t.Fatal("readiness")
			}
			folders := execute(t, s, api.Command{Operation: api.OperationMailboxes, MailboxId: "mailbox"})
			if len(folders.Mailboxes) != 3 || slices.Contains(folders.Mailboxes, "Private") {
				t.Fatal("folder visibility")
			}
			fullScope := auth.mutate
			auth.mutate = func(d *api.AuthorizationDecision) { fullScope(d); d.UserScope.Folders = []string{"Archive"} }
			limited := execute(t, s, api.Command{Operation: api.OperationMailboxes, MailboxId: "mailbox"})
			if len(limited.Mailboxes) != 1 || limited.Mailboxes[0] != "Archive" {
				t.Fatal("non-default folder discovery")
			}
			auth.mutate = fullScope
			first := execute(t, s, api.Command{Operation: api.OperationList, MailboxId: "mailbox"})
			if len(first.Headers) != 1 || first.Headers[0].Uid != "3" || first.UidValidity == 0 || first.NextCursor == "" {
				t.Fatalf("first page: %+v", first)
			}
			second := execute(t, s, api.Command{Operation: api.OperationList, MailboxId: "mailbox", Cursor: first.NextCursor})
			if len(second.Headers) != 1 || second.Headers[0].Uid != "2" {
				t.Fatal("next page")
			}
			for _, op := range []api.Operation{api.OperationFetch, api.OperationDownload, api.OperationAttachments} {
				r := execute(t, s, api.Command{Operation: op, MailboxId: "mailbox", Uid: "3", UidValidity: first.UidValidity})
				if len(r.Attachments) != 1 || r.ContentDigest == "" {
					t.Fatal("MIME result")
				}
				if op == api.OperationAttachments && r.Attachments[0].ContentBase64 != "" {
					t.Fatal("attachment listing disclosed content")
				}
			}
			after := execute(t, s, api.Command{Operation: api.OperationList, MailboxId: "mailbox"})
			if slices.Contains(after.Headers[0].Flags, "\\Seen") {
				t.Fatal("read changed seen flag")
			}
			for _, op := range []api.Operation{api.OperationSearch, api.OperationThread} {
				r := execute(t, s, api.Command{Operation: op, MailboxId: "mailbox", Query: "Original body", ThreadId: "source@example.test"})
				if len(r.Headers) != 1 {
					t.Fatal("native search")
				}
			}
			partialThread := execute(t, s, api.Command{Operation: api.OperationThread, MailboxId: "mailbox", ThreadId: "source"})
			if len(partialThread.Headers) != 0 || len(partialThread.Messages) != 0 {
				t.Fatal("thread substring treated as exact Message-ID")
			}
			for _, op := range []api.Operation{api.OperationMarkRead, api.OperationMarkUnread} {
				cmd := api.Command{Operation: op, MailboxId: "mailbox", Uid: "3", UidValidity: first.UidValidity, EffectKey: string(op)}
				result := execute(t, s, cmd)
				if result.Status != "accepted" || execute(t, s, cmd).MessageId != result.MessageId {
					t.Fatal("flag receipt")
				}
				flags := execute(t, s, api.Command{Operation: api.OperationList, MailboxId: "mailbox"}).Headers[0].Flags
				if slices.Contains(flags, "\\Seen") != (op == api.OperationMarkRead) {
					t.Fatal("flag state")
				}
			}
			draft := send(api.OperationDraftCreate, "draft-create")
			created := execute(t, s, draft)
			if created.Status != "accepted" || created.Uid == "" || created.Folder != "Drafts" || created.ContentDigest == "" {
				t.Fatalf("draft result: %+v", created)
			}
			if duplicate := execute(t, s, draft); api.Digest(duplicate) != api.Digest(created) {
				t.Fatal("draft receipt replay")
			}
			update := send(api.OperationDraftUpdate, "draft-update")
			update.Uid = created.Uid
			update.UidValidity = created.UidValidity
			update.ExpectedDigest = created.ContentDigest
			update.Message.BodyText = "Updated draft"
			updated := execute(t, s, update)
			if updated.Status != "accepted" || updated.Uid == created.Uid {
				t.Fatalf("draft replacement: %+v", updated)
			}
			readDraft := execute(t, s, api.Command{Operation: api.OperationFetch, MailboxId: "mailbox", Folder: "Drafts", Uid: updated.Uid, UidValidity: updated.UidValidity})
			if !strings.Contains(readDraft.BodyText, "Updated draft") {
				t.Fatal("draft body")
			}
			deleted := execute(t, s, api.Command{Operation: api.OperationDraftDelete, MailboxId: "mailbox", Uid: updated.Uid, UidValidity: updated.UidValidity, EffectKey: "draft-delete"})
			if deleted.Status != "deleted" {
				t.Fatal("draft deletion")
			}
			for _, op := range []api.Operation{api.OperationReply, api.OperationReplyAll, api.OperationForward} {
				cmd := send(op, string(op))
				cmd.Message.SourceUid = "3"
				cmd.Message.SourceUidValidity = first.UidValidity
				if op == api.OperationReplyAll {
					cmd.Message.Cc = []string{"copy@example.test"}
				}
				if execute(t, s, cmd).Status != "accepted" {
					t.Fatal("SMTP with IMAP source")
				}
			}
			extraRecipient := send(api.OperationReply, "reply-extra-recipient")
			extraRecipient.Message.SourceUid = "3"
			extraRecipient.Message.SourceUidValidity = first.UidValidity
			extraRecipient.Message.Cc = []string{"copy@example.test"}
			if execute(t, s, extraRecipient).Status != "accepted" {
				t.Fatal("authorized extra reply recipient rejected")
			}
			for i, op := range []api.Operation{api.OperationMove, api.OperationArchive, api.OperationDelete} {
				cmd := api.Command{Operation: op, MailboxId: "mailbox", Uid: []string{"1", "2", "3"}[i], UidValidity: first.UidValidity, EffectKey: string(op)}
				if op == api.OperationMove {
					cmd.DestinationFolder = "Archive"
				}
				r := execute(t, s, cmd)
				if op == api.OperationDelete {
					if r.Status != "deleted" {
						t.Fatal("UID delete")
					}
				} else if r.Status != "accepted" || r.Folder != "Archive" || r.Uid == "" {
					t.Fatalf("move result: %+v", r)
				}
			}
			reads := sec.reads.Load()
			if _, err := s.Execute(t.Context(), "caller", "token", api.Command{Operation: api.OperationList, MailboxId: "mailbox", Folder: "Private"}); !errors.Is(err, errs.Denied) {
				t.Fatal("foreign folder accepted")
			}
			if sec.reads.Load() != reads {
				t.Fatal("credentials before folder scope")
			}
			if _, err := s.Execute(t.Context(), "caller", "token", api.Command{Operation: api.OperationList, MailboxId: "mailbox", Folder: "Archive", Cursor: first.NextCursor}); !errors.Is(err, errs.Conflict) {
				t.Fatal("cross-folder cursor accepted")
			}
			if err := user.Delete("INBOX"); err != nil {
				t.Fatal(err)
			}
			if err := user.Create("INBOX", nil); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Execute(t.Context(), "caller", "token", api.Command{Operation: api.OperationFetch, MailboxId: "mailbox", Uid: "3", UidValidity: first.UidValidity}); !errors.Is(err, errs.Conflict) {
				t.Fatal("stale UIDVALIDITY accepted")
			}
		})
	}
}
