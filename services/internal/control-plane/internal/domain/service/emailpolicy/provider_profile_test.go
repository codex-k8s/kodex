package emailpolicy

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestProviderProfileRequiresDocumentedTLSAndPassword separates application passwords from OAuth and rejects alternate hosts, weaker transport and unsupported POP substitutions before publication.
// TestProviderProfileRequiresDocumentedTLSAndPassword отделяет пароль приложения от OAuth и до публикации отклоняет иные hosts, ослабленный transport и неподдержанные подстановки POP.
func TestProviderProfileRequiresDocumentedTLSAndPassword(t *testing.T) {
	for _, profile := range []string{ProviderTimewebPassword, ProviderGmailAppPassword} {
		t.Run(profile, func(t *testing.T) {
			spec := mailboxSpecificationFixture(t)
			spec.ProviderProfile = profile
			spec.POP = nil
			spec.SMTP.Host, spec.SMTP.ServerName = "smtp.timeweb.ru", "smtp.timeweb.ru"
			spec.IMAP.Host, spec.IMAP.ServerName = "imap.timeweb.ru", "imap.timeweb.ru"
			if profile == ProviderGmailAppPassword {
				spec.SMTP.Host, spec.SMTP.ServerName = "smtp.gmail.com", "smtp.gmail.com"
				spec.IMAP.Host, spec.IMAP.ServerName = "imap.gmail.com", "imap.gmail.com"
			}
			spec.SMTP.Port, spec.SMTP.TlsMode, spec.SMTP.AuthMethod = 465, "implicit", "password"
			spec.IMAP.Port, spec.IMAP.TlsMode, spec.IMAP.AuthMethod = 993, "implicit", "password"
			binding := MailboxBinding{Ref: "server-mailbox", OrganizationRef: "server-tenant", ConnectionRef: "server-connection", Revision: 1, CredentialGeneration: 1}
			if _, err := MaterializeMailbox(spec, binding); err != nil {
				t.Fatal("documented profile rejected", err)
			}
			startTLS := spec
			startTLS.SMTP.Port, startTLS.SMTP.TlsMode = 587, "starttls"
			if ValidateProviderProfile(startTLS) != nil {
				t.Fatal("SMTP STARTTLS profile rejected")
			}
			for _, mutate := range []func(*entity.EmailMailboxSpecification){
				func(s *entity.EmailMailboxSpecification) { s.SMTP.Host = "other.example.test" },
				func(s *entity.EmailMailboxSpecification) { s.SMTP.ServerName = "other.example.test" },
				func(s *entity.EmailMailboxSpecification) { s.SMTP.AuthMethod = "oauthbearer" },
				func(s *entity.EmailMailboxSpecification) { s.SMTP.Port = 25 },
				func(s *entity.EmailMailboxSpecification) { s.SMTP.TlsMode = "starttls" },
				func(s *entity.EmailMailboxSpecification) { s.IMAP = nil },
				func(s *entity.EmailMailboxSpecification) { s.ReceiveProtocol = "pop3" },
				func(s *entity.EmailMailboxSpecification) { s.ProviderProfile = "GMAIL_OAUTH2" },
			} {
				invalid := spec
				mutate(&invalid)
				if _, err := MaterializeMailbox(invalid, binding); !errors.Is(err, errs.ErrInvalid) {
					t.Fatalf("invalid provider profile accepted: %v", err)
				}
			}
			imap := *spec.IMAP
			imap.Port, imap.TlsMode = 143, "starttls"
			spec.IMAP = &imap
			if (ValidateProviderProfile(spec) == nil) != (profile == ProviderTimewebPassword) {
				t.Fatal("IMAP profile port matrix changed")
			}
		})
	}
}

// TestProviderProfileDraftRemainsExplicit permits incomplete known drafts while refusing unknown profile aliases and preserving the historical custom endpoint path.
// TestProviderProfileDraftRemainsExplicit допускает неполные известные черновики, отклоняет неизвестные псевдонимы и сохраняет исторический путь собственных endpoints.
func TestProviderProfileDraftRemainsExplicit(t *testing.T) {
	for _, profile := range []string{"", ProviderCustom, ProviderTimewebPassword, ProviderGmailAppPassword} {
		if !KnownProviderProfile(profile) {
			t.Fatal("known draft profile rejected")
		}
	}
	for _, profile := range []string{"gmail_app_password", "GMAIL", "OAUTHBEARER", " GMAIL_APP_PASSWORD", "GMAIL_APP_PASSWORD\x00"} {
		if KnownProviderProfile(profile) {
			t.Fatal("profile alias accepted")
		}
	}
	if _, err := DecodeSpecification("JSON", `{"provider_profile":"GMAIL_OAUTH2"}`); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("unknown draft profile accepted")
	}
	for _, profile := range []string{"", ProviderCustom} {
		spec := mailboxSpecificationFixture(t)
		spec.ProviderProfile = profile
		if ValidateProviderProfile(spec) != nil {
			t.Fatal("historical custom profile narrowed")
		}
	}
}
