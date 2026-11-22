package component

import (
	"crypto/tls"
	"sync/atomic"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/mailtransport"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
)

// personalPasswordIMAPSession records real LOGIN calls only after the fixture TLS handshake;
// the embedded native IMAP session still validates the actual username and password.
// personalPasswordIMAPSession учитывает реальные LOGIN calls только после fixture TLS handshake;
// встроенная native IMAP session по-прежнему проверяет настоящие username и password.
type personalPasswordIMAPSession struct {
	imapserver.Session
	connection *imapserver.Conn
	accepted   *atomic.Int32
	cleartext  *atomic.Int32
}

// Login observes encryption and successful password authentication without logging wire bytes
// or credentials; it neither implements OAUTHBEARER nor pretends to provide Gmail XOAUTH2.
// Login наблюдает шифрование и успешную password authentication без логирования wire bytes
// либо credentials; не реализует OAUTHBEARER и не выдаёт его за Gmail XOAUTH2.
func (session *personalPasswordIMAPSession) Login(username, password string) error {
	connection, secured := session.connection.NetConn().(*tls.Conn)
	if !secured || !connection.ConnectionState().HandshakeComplete {
		session.cleartext.Add(1)
	}
	err := session.Session.Login(username, password)
	if err == nil {
		session.accepted.Add(1)
	}
	return err
}

// Move preserves the native extension already advertised by the wrapped fixture backend; the
// authentication observer must not erase SessionMove while leaving the MOVE capability enabled.
// Move сохраняет native extension, уже объявленное fixture backend; наблюдатель authentication
// не удаляет SessionMove при включённой capability MOVE.
func (session *personalPasswordIMAPSession) Move(writer *imapserver.MoveWriter, set imap.NumSet, folder string) error {
	return session.Session.(imapserver.SessionMove).Move(writer, set, folder)
}

// TestPersonalPasswordProfilesNativeTLS exercises the real SMTP/IMAP adapter against loopback
// TLS listeners using exact Gmail/Timeweb names, implicit/STARTTLS SMTP, implicit Gmail IMAP
// and both supported Timeweb IMAP modes. It
// proves PLAIN/LOGIN rather than OAuth substitution, bad CA/SNI refusal and zero message effects;
// in-memory owner/receipt fixtures do not qualify real account access or PostgreSQL durability.
// TestPersonalPasswordProfilesNativeTLS проверяет настоящий SMTP/IMAP adapter на loopback TLS
// listeners с точными Gmail/Timeweb names, implicit/STARTTLS SMTP, implicit Gmail IMAP
// и обоими поддерживаемыми режимами Timeweb IMAP. Доказывает
// PLAIN/LOGIN вместо подмены OAuth, отказ bad CA/SNI и отсутствие message effects; in-memory
// owner/receipt fixtures не подтверждают доступ к реальным аккаунтам либо PostgreSQL durability.
func TestPersonalPasswordProfilesNativeTLS(t *testing.T) {
	for _, provider := range []struct {
		name, suffix string
		imapModes    []string
	}{{"gmail-app-password", "gmail.com", []string{"implicit"}}, {"timeweb-password", "timeweb.ru", []string{"implicit", "starttls"}}} {
		for _, mode := range []string{"implicit", "starttls"} {
			for _, imapMode := range provider.imapModes {
				t.Run(provider.name+"/"+mode+"-smtp/"+imapMode+"-imap", func(t *testing.T) {
					smtpName, imapName := "smtp."+provider.suffix, "imap."+provider.suffix
					fixture := newFixture(t, mode, smtpName, imapName, "localhost")
					var logins, cleartext atomic.Int32
					_, address := imapFixture(t, fixture, imapMode, func(session imapserver.Session, connection *imapserver.Conn) imapserver.Session {
						return &personalPasswordIMAPSession{Session: session, connection: connection, accepted: &logins, cleartext: &cleartext}
					})
					bridge, secrets, _ := service(t, fixture, mode, nil)
					mailbox := &bridge.Config.Mailboxes[0]
					mailbox.CredentialOwnerGeneration = 1
					mailbox.Smtp.Host, mailbox.Smtp.ServerName = smtpName, smtpName
					endpoint := mailbox.Smtp
					endpoint.Host, endpoint.ServerName, endpoint.Port, endpoint.TlsMode = imapName, imapName, 993, api.EndpointTlsMode(imapMode)
					if imapMode == "starttls" {
						endpoint.Port = 143
					}
					mailbox.Imap, mailbox.Pop, mailbox.ReceiveProtocol = &endpoint, nil, "imap"
					bridge.Provider = &mailtransport.Provider{Secrets: secrets, Dialer: imapDialFixture{dialFixture{fixture.smtp, fixture.pop}, address}}
					response := execute(t, bridge, api.Command{Operation: api.OperationHealth, MailboxId: mailbox.Id})
					if response.Status != "ready" || response.ProtocolReadiness == nil || response.ProtocolReadiness.Smtp != "ready" || response.ProtocolReadiness.Imap != "ready" || response.ProtocolReadiness.Pop3 != "not_configured" || fixture.plainAuth.Load() != 1 || fixture.oauthAuth.Load() != 0 || logins.Load() != 1 || fixture.insecureAuth.Load() != 0 || cleartext.Load() != 0 {
						t.Fatal("personal password profile did not use verified TLS PLAIN/LOGIN")
					}
					mailbox.Imap.ServerName = "wrong.example.test"
					response = execute(t, bridge, api.Command{Operation: api.OperationHealth, MailboxId: mailbox.Id})
					if response.Status != "not_ready" || response.ProtocolReadiness == nil || response.ProtocolReadiness.Imap != "not_ready" || response.ProtocolReadiness.Smtp != "ready" || logins.Load() != 1 {
						t.Fatal("wrong IMAP SNI reached password authentication")
					}
					mailbox.Imap.ServerName = imapName
					_, wrongCA := certificate(t)
					secrets.ca = wrongCA
					before := fixture.plainAuth.Load()
					response = execute(t, bridge, api.Command{Operation: api.OperationHealth, MailboxId: mailbox.Id})
					if response.Status != "not_ready" || response.ProtocolReadiness == nil || response.ProtocolReadiness.Imap != "not_ready" || response.ProtocolReadiness.Smtp != "not_ready" || fixture.plainAuth.Load() != before || logins.Load() != 1 {
						t.Fatal("untrusted CA reached password authentication")
					}
					fixture.mu.Lock()
					defer fixture.mu.Unlock()
					if len(fixture.sent) != 0 || fixture.deletes != 0 || fixture.insecureAuth.Load() != 0 || cleartext.Load() != 0 {
						t.Fatal("password profile readiness created a message effect or plaintext authentication")
					}
				})
			}
		}
	}
}
