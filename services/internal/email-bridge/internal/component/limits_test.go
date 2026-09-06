package component

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	httptransport "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/transport/http"
)

func TestMessageLimitsBeforeCredentials(t *testing.T) {
	for name, mutate := range map[string]func(*api.Command){
		"body": func(c *api.Command) { c.Message.BodyText = strings.Repeat("x", 65537) },
		"attachment": func(c *api.Command) {
			c.Message.Attachments[0].ContentBase64 = base64.StdEncoding.EncodeToString(make([]byte, 32769))
		},
		"attachment-count": func(c *api.Command) { c.Message.Attachments = make([]api.Attachment, 6) },
		"recipient-count": func(c *api.Command) {
			for range 10 {
				c.Message.Cc = append(c.Message.Cc, "copy@example.test")
			}
		},
		"combined": func(c *api.Command) { c.Message.BodyText = strings.Repeat("x", 65536) },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, "implicit")
			s, secrets, _ := service(t, f, "implicit", nil)
			command := send(api.OperationSend, name)
			mutate(&command)
			_, err := s.Execute(executionContext(t.Context()), httptransport.CallerSPIFFE, "token", command)
			if !errors.Is(err, errs.Invalid) || secrets.reads.Load() != 0 {
				t.Fatal("message limit must precede credentials")
			}
		})
	}
}

func TestPOPBoundsAndSnapshotValidation(t *testing.T) {
	for _, name := range []string{"advertised-size", "actual-size", "duplicate-number", "duplicate-uid", "duplicate-zero-size", "non-ascii-uid", "scan-limit"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, "implicit")
			s, _, _ := service(t, f, "implicit", nil)
			f.mu.Lock()
			switch name {
			case "advertised-size":
				f.listLines = "1 999999\r\n2 999999\r\n"
			case "actual-size":
				f.listLines = "1 1\r\n2 1\r\n"
				f.raw += strings.Repeat("x", 65537)
			case "duplicate-number":
				f.uidlLines = "1 uid-one\r\n1 uid-two\r\n"
			case "duplicate-uid":
				f.uidlLines = "1 uid-one\r\n2 uid-one\r\n"
			case "duplicate-zero-size":
				f.uidlLines = "1 uid-one\r\n1 uid-two\r\n"
				f.listLines = "1 0\r\n1 0\r\n"
			case "non-ascii-uid":
				f.uidlLines = "1 uid-\xc3\xa9\r\n2 uid-two\r\n"
			case "scan-limit":
				s.Config.Mailboxes[0].Limits.ScanMessages = 1
			}
			f.mu.Unlock()
			_, err := s.Execute(executionContext(t.Context()), httptransport.CallerSPIFFE, "token", api.Command{Operation: api.OperationFetch, MailboxId: "mailbox", Uid: "uid-one"})
			if err == nil {
				t.Fatal("invalid POP snapshot or size accepted")
			}
			if name != "actual-size" && f.retrievals.Load() != 0 {
				t.Fatal("invalid snapshot must precede RETR")
			}
		})
	}
}

func TestPaginationSourceBinding(t *testing.T) {
	f := newFixture(t, "implicit")
	s, _, _ := service(t, f, "implicit", nil)
	mailbox := s.Config.Mailboxes[0]
	first, err := s.Provider.Read(t.Context(), mailbox, api.Command{Operation: api.OperationList})
	if err != nil || first.NextCursor == "" {
		t.Fatal("missing first page")
	}
	for name, mutate := range map[string]func(*api.Mailbox){
		"tenant":     func(m *api.Mailbox) { m.TenantId = "other" },
		"mailbox":    func(m *api.Mailbox) { m.Id = "other" },
		"connection": func(m *api.Mailbox) { m.ConnectionId = "other" },
		"revision":   func(m *api.Mailbox) { m.Revision++ },
	} {
		t.Run(name, func(t *testing.T) {
			other := mailbox
			mutate(&other)
			_, err := s.Provider.Read(t.Context(), other, api.Command{Operation: api.OperationList, Cursor: first.NextCursor})
			if !errors.Is(err, errs.Conflict) {
				t.Fatal("cursor crossed source boundary")
			}
		})
	}
	if _, err := s.Provider.Read(t.Context(), mailbox, api.Command{Operation: api.OperationSearch, Query: "different", Cursor: first.NextCursor}); !errors.Is(err, errs.Conflict) {
		t.Fatal("cursor crossed query boundary")
	}
	second, err := s.Provider.Read(t.Context(), mailbox, api.Command{Operation: api.OperationList, Cursor: first.NextCursor})
	if err != nil || len(second.Headers) != 1 || second.NextCursor != "" || second.Headers[0].Uid == first.Headers[0].Uid {
		t.Fatal("stable pagination failed")
	}
}
