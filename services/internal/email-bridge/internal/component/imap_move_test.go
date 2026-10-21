package component

import (
	"sync/atomic"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/mailtransport"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
)

// copyFailureSession can reject IMAP COPY and count subsequent STORE calls,
// exposing whether the non-native MOVE fallback modifies a source too early.
// copyFailureSession умеет отклонить IMAP COPY и считать последующие STORE,
// выявляя преждевременное изменение источника при запасной реализации MOVE.
type copyFailureSession struct {
	imapserver.Session
	reject bool
	stores *atomic.Int32
}

// Copy injects an explicit protocol refusal or delegates to the in-memory IMAP
// session; rejected copying must leave the source untouched by later steps.
// Copy возвращает явный отказ протокола либо делегирует IMAP-сессии в памяти;
// при отказе копирования дальнейшие шаги должны оставить источник неизменным.
func (s *copyFailureSession) Copy(set imap.NumSet, destination string) (*imap.CopyData, error) {
	if s.reject {
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Fixture COPY rejected"}
	}
	return s.Session.Copy(set, destination)
}

// Store increments the shared observation counter before forwarding the flag
// update, letting tests detect a deletion attempt even if the delegate fails.
// Store увеличивает общий счётчик перед передачей изменения флагов, позволяя
// обнаружить попытку удаления даже при отказе обёрнутой сессии.
func (s *copyFailureSession) Store(w *imapserver.FetchWriter, set imap.NumSet, flags *imap.StoreFlags, opts *imap.StoreOptions) error {
	s.stores.Add(1)
	return s.Session.Store(w, set, flags, opts)
}

// TestIMAPMoveFallbackWaitsForCopy disables native MOVE and verifies source
// deletion happens only after COPY succeeds, using both counters and mailbox sizes.
// TestIMAPMoveFallbackWaitsForCopy отключает нативный MOVE и по счётчикам/размерам
// ящиков проверяет удаление источника только после успешного COPY.
func TestIMAPMoveFallbackWaitsForCopy(t *testing.T) {
	for _, reject := range []bool{false, true} {
		name := "copy-accepted"
		if reject {
			name = "copy-rejected"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, "implicit")
			var stores atomic.Int32
			user, address := imapFixtureCaps(t, f, "implicit", false, func(session imapserver.Session, _ *imapserver.Conn) imapserver.Session {
				return &copyFailureSession{session, reject, &stores}
			})
			s, sec, auth := service(t, f, "implicit", nil)
			m := &s.Config.Mailboxes[0]
			endpoint := m.Smtp
			endpoint.Port = 993
			m.Imap = &endpoint
			m.ReceiveProtocol = "imap"
			m.AllowedFolders = []string{"INBOX", "Archive"}
			auth.mutate = func(d *api.AuthorizationDecision) {
				for _, scope := range []*api.Scope{&d.UserScope, &d.AgentScope, &d.ConnectionScope, &d.ResourceScope} {
					scope.Folders = m.AllowedFolders
				}
			}
			s.Provider = &mailtransport.Provider{Secrets: sec, Dialer: imapDialFixture{dialFixture{f.smtp, f.pop}, address}}
			r := execute(t, s, api.Command{Operation: api.OperationMove, MailboxId: "mailbox", Uid: "1", UidValidity: 1, DestinationFolder: "Archive", EffectKey: "move"})
			source, err := user.Status("INBOX", &imap.StatusOptions{NumMessages: true})
			if err != nil {
				t.Fatal(err)
			}
			dest, err := user.Status("Archive", &imap.StatusOptions{NumMessages: true})
			if err != nil {
				t.Fatal(err)
			}
			if reject {
				if r.Status != "unknown" || stores.Load() != 0 || *source.NumMessages != 3 || *dest.NumMessages != 0 {
					t.Fatal("COPY rejection changed source")
				}
			} else if r.Status != "accepted" || stores.Load() != 1 || *source.NumMessages != 2 || *dest.NumMessages != 1 {
				t.Fatal("sequential move fallback")
			}
		})
	}
}
