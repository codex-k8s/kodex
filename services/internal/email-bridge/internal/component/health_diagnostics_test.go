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

// TestHealthProtocolDiagnostics classifies synthetic POP readiness failures
// separately from SMTP and checks safe JSON diagnostics without message effects.
// TestHealthProtocolDiagnostics классифицирует тестовые сбои готовности POP
// отдельно от SMTP и проверяет безопасный JSON без изменений писем.
func TestHealthProtocolDiagnostics(t *testing.T) {
	cases := []struct {
		name   string
		reason api.ProtocolReadinessReason
	}{
		{"ready", api.ProtocolReadinessReasonNone}, {"auth", api.ProtocolReadinessReasonAuthRejected}, {"tls", api.ProtocolReadinessReasonTLSUnavailable}, {"credential", api.ProtocolReadinessReasonCredentialUnavailable}, {"configuration", api.ProtocolReadinessReasonConfigurationInvalid}, {"network", api.ProtocolReadinessReasonNetworkUnavailable},
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

// TestPOPHealthDoesNotEnumerateMaildrop supplies invalid UIDL/LIST data and a low
// scan limit, requiring readiness to succeed without retrieving or enumerating messages.
// TestPOPHealthDoesNotEnumerateMaildrop задаёт неверные UIDL/LIST и низкий лимит,
// требуя успешной готовности без получения и перечисления писем.
func TestPOPHealthDoesNotEnumerateMaildrop(t *testing.T) {
	f := newFixture(t, "implicit")
	f.uidlLines = "bad uidl fixture-private-text\r\n"
	f.listLines = "1 -2\r\n2 3\r\n"
	s, _, _ := service(t, f, "implicit", nil)
	s.Config.Mailboxes[0].Limits.ScanMessages = 1
	result := execute(t, s, api.Command{Operation: api.OperationHealth, MailboxId: "mailbox"})
	if result.Status != "ready" || result.ProtocolReadiness.Pop3 != "ready" || f.retrievals.Load() != 0 {
		t.Fatal("health enumerated or read the POP3 maildrop")
	}
}

// failingHealthDial injects a POP connection error while preserving the original
// SMTP route, allowing per-protocol readiness and redaction assertions.
// failingHealthDial создаёт ошибку соединения POP, сохраняя исходный SMTP-маршрут
// для проверки раздельной готовности протоколов и сокрытия подробностей ошибки.
type failingHealthDial struct{ original mailtransport.Dialer }

// Dial fails only the synthetic POP TLS port with private fixture text and
// delegates every other target to the wrapped test dialer.
// Dial отклоняет только тестовый TLS-порт POP с условным закрытым текстом ошибки,
// а остальные адреса передаёт обёрнутому тестовому соединителю.
func (d failingHealthDial) Dial(ctx context.Context, target string) (net.Conn, error) {
	if strings.HasSuffix(target, ":995") {
		return nil, errors.New("fixture-private-text")
	}
	return d.original.Dial(ctx, target)
}

// rejectedHealthIMAP wraps a local IMAP session to inject either authentication
// refusal or an incomplete SELECT response for readiness classification.
// rejectedHealthIMAP оборачивает локальную IMAP-сессию для отказа аутентификации
// либо неполного ответа SELECT при проверке классификации готовности.
type rejectedHealthIMAP struct {
	imapserver.Session
	invalidSelect bool
}

// Login rejects the authentication scenario with private fixture text; the
// malformed-SELECT scenario authenticates through the wrapped session instead.
// Login отклоняет сценарий аутентификации с условным закрытым текстом; сценарий
// некорректного SELECT проходит вход через обёрнутую сессию.
func (s *rejectedHealthIMAP) Login(username, password string) error {
	if !s.invalidSelect {
		return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "fixture-private-text"}
	}
	return s.Session.Login(username, password)
}

// Select deliberately returns empty metadata with no error so readiness must
// recognize an invalid response rather than falsely report an authenticated mailbox ready.
// Select намеренно возвращает пустые метаданные без ошибки, чтобы проверка готовности
// распознала неверный ответ, а не объявила ящик готовым только по успешному входу.
func (s *rejectedHealthIMAP) Select(folder string, options *imap.SelectOptions) (*imap.SelectData, error) {
	return &imap.SelectData{}, nil
}

// TestIMAPHealthProtocolDiagnostics distinguishes authentication, malformed
// SELECT and TLS-name failures on a local IMAP server while SMTP remains ready.
// TestIMAPHealthProtocolDiagnostics различает ошибки входа, неполный SELECT
// и неверное TLS-имя локального IMAP-сервера при сохраняющейся готовности SMTP.
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

// TestSMTPAndReceiveHealthFailuresRemainSeparate retains simultaneous SMTP/POP
// authentication reasons and excludes provider-private text from the health summary.
// TestSMTPAndReceiveHealthFailuresRemainSeparate сохраняет одновременные причины
// отказа входа SMTP/POP и исключает закрытый текст провайдера из сводки готовности.
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
