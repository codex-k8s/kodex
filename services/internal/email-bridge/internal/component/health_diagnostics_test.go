package component

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/mailtransport"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"net"
	"strings"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
)

func TestHealthProtocolDiagnostics(t *testing.T) {
	cases := []struct {
		name   string
		reason api.ProtocolReadinessReason
	}{
		{"ready", api.ProtocolReadinessReasonNone}, {"auth", api.ProtocolReadinessReasonAuthRejected}, {"tls", api.ProtocolReadinessReasonTLSUnavailable}, {"credential", api.ProtocolReadinessReasonCredentialUnavailable}, {"configuration", api.ProtocolReadinessReasonConfigurationInvalid}, {"network", api.ProtocolReadinessReasonNetworkUnavailable}, {"uidl", api.ProtocolReadinessReasonResponseInvalid}, {"list", api.ProtocolReadinessReasonResponseInvalid}, {"limit", api.ProtocolReadinessReasonScanLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, "implicit")
			s, sec, _ := service(t, f, "implicit", nil)
			m := &s.Config.Mailboxes[0]
			switch tc.name {
			case "network":
				s.Provider = &mailtransport.Provider{Secrets: sec, Dialer: failingHealthDial{dialFixture{f.smtp, f.pop}}}
			case "auth":
				f.rejectPOPAuth.Store(true)
			case "tls":
				m.Pop.ServerName = "wrong.example.test"
			case "configuration":
				m.Pop.Ca.Name = "bad-ca"
			case "credential":
				m.Pop.Username.Name = "missing"
			case "uidl":
				f.uidlLines = "bad uidl fixture-private-text\r\n"
			case "list":
				f.listLines = "1 -2\r\n2 3\r\n"
			case "limit":
				m.Limits.ScanMessages = 1
			}
			result := execute(t, s, api.Command{Operation: api.OperationHealth, MailboxId: "mailbox"})
			if result.ProtocolReadiness.Smtp != "ready" || *result.ProtocolReadiness.Pop3Reason != tc.reason {
				t.Fatalf("diagnostic mismatch: %s %s", result.ProtocolReadiness.Smtp, *result.ProtocolReadiness.Pop3Reason)
			}
			b, _ := json.Marshal(result)
			for _, value := range []string{"fixture-private-text", "wrong.example.test", "fixture-value"} {
				if strings.Contains(string(b), value) {
					t.Fatal("private detail leaked")
				}
			}
			if f.retrievals.Load() != 0 || f.deletes != 0 || len(f.sent) != 0 {
				t.Fatal("health performed message effect")
			}
		})
	}
}

type failingHealthDial struct{ original mailtransport.Dialer }

func (d failingHealthDial) Dial(ctx context.Context, target string) (net.Conn, error) {
	if strings.HasSuffix(target, ":995") {
		return nil, errors.New("fixture-private-text")
	}
	return d.original.Dial(ctx, target)
}

type rejectedHealthIMAP struct {
	imapserver.Session
	invalidSelect bool
}

func (s *rejectedHealthIMAP) Login(username, password string) error {
	if !s.invalidSelect {
		return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "fixture-private-text"}
	}
	return s.Session.Login(username, password)
}
func (s *rejectedHealthIMAP) Select(folder string, options *imap.SelectOptions) (*imap.SelectData, error) {
	return &imap.SelectData{}, nil
}
func TestIMAPHealthProtocolDiagnostics(t *testing.T) {
	for _, kind := range []string{"auth", "response", "tls"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, "implicit")
			_, address := imapFixtureCaps(t, f, "implicit", false, func(s imapserver.Session, _ *imapserver.Conn) imapserver.Session {
				return &rejectedHealthIMAP{s, kind != "auth"}
			})
			s, sec, _ := service(t, f, "implicit", nil)
			m := &s.Config.Mailboxes[0]
			e := m.Smtp
			e.Port = 993
			m.Imap = &e
			m.Pop = nil
			m.ReceiveProtocol = "imap"
			expected := api.ProtocolReadinessReasonAuthRejected
			if kind == "response" {
				expected = api.ProtocolReadinessReasonResponseInvalid
			}
			if kind == "tls" {
				m.Imap.ServerName = "wrong.example.test"
				expected = api.ProtocolReadinessReasonTLSUnavailable
			}
			s.Provider = &mailtransport.Provider{Secrets: sec, Dialer: imapDialFixture{dialFixture{f.smtp, f.pop}, address}}
			result := execute(t, s, api.Command{Operation: api.OperationHealth, MailboxId: "mailbox"})
			if result.ProtocolReadiness.Smtp != "ready" || *result.ProtocolReadiness.ImapReason != expected {
				t.Fatalf("IMAP reason mismatch %s", *result.ProtocolReadiness.ImapReason)
			}
		})
	}
}

func TestSMTPAndReceiveHealthFailuresRemainSeparate(t *testing.T) {
	f := newFixture(t, "implicit")
	f.rejectSMTPAuth.Store(true)
	f.rejectPOPAuth.Store(true)
	s, _, _ := service(t, f, "implicit", nil)
	r := execute(t, s, api.Command{Operation: api.OperationHealth, MailboxId: "mailbox"})
	if r.Status != "not_ready" || *r.ProtocolReadiness.SmtpReason != api.ProtocolReadinessReasonAuthRejected || *r.ProtocolReadiness.Pop3Reason != api.ProtocolReadinessReasonAuthRejected {
		t.Fatal("simultaneous reasons lost")
	}
	summary, err := api.HealthSummary(r.Status, r.ProtocolReadiness)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(summary, "fixture-private-text") {
		t.Fatal("raw provider error leaked")
	}
}
