package httptransport

import (
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// TestMailboxJSONRejectsAmbiguousRequests exercises the actual mailbox HTTP syntax boundary with
// generated nested DTOs before any owner/materializer call; ordinary optional omission stays valid.
// TestMailboxJSONRejectsAmbiguousRequests проверяет реальную HTTP-границу mailbox с generated
// вложенными DTO до вызова владельца/materializer; обычное отсутствие optional полей допустимо.
func TestMailboxJSONRejectsAmbiguousRequests(t *testing.T) {
	for name, body := range map[string]string{
		"case alias":                   `{"Yaml":"enabled: true"}`,
		"case collision":               `{"yaml":"first","Yaml":"second"}`,
		"duplicate":                    `{"yaml":"first","yaml":"second"}`,
		"explicit null":                `{"yaml":null}`,
		"nested null":                  `{"specification":{"enabled":null}}`,
		"nested alias":                 `{"specification":{"Enabled":true}}`,
		"policy alias":                 `{"specification":{"policies":[{"Policy":"ALLOW"}]}}`,
		"oversize trailing whitespace": `{"yaml":"draft"}` + strings.Repeat(" ", int(maximumJSONBody)),
	} {
		t.Run(name, func(t *testing.T) {
			writer := httptest.NewRecorder()
			request := httptest.NewRequest("POST", "/", strings.NewReader(body))
			value, ok := decodeMailboxJSON[generated.EmailMailboxDraftContent](writer, request)
			if ok || writer.Code != 400 || value.Yaml != nil || value.Specification != nil {
				t.Fatal("ambiguous mailbox body reached semantic use")
			}
		})
	}
	for _, body := range []string{`{"yaml":"enabled: false"}`, `{"specification":{"enabled":false,"allowedFolders":[]}}`} {
		writer := httptest.NewRecorder()
		if _, ok := decodeMailboxJSON[generated.EmailMailboxDraftContent](writer, httptest.NewRequest("POST", "/", strings.NewReader(body))); !ok {
			t.Fatal("ordinary mailbox DTO rejected")
		}
	}
	writer := httptest.NewRecorder()
	if _, ok := decodeMailboxJSON[personalEmailCreateBody](writer, httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"Личная почта","sender":"owner@example.test"}`))); !ok {
		t.Fatal("plain self mailbox creation body rejected")
	}
}

// TestMailboxProviderProfileMapping preserves the explicit provider profile in both directions;
// unknown profiles fail rather than silently becoming the historical custom endpoint profile.
// TestMailboxProviderProfileMapping сохраняет явный профиль провайдера в обоих направлениях;
// неизвестный профиль отклоняется вместо скрытой замены историческим custom endpoint profile.
func TestMailboxProviderProfileMapping(t *testing.T) {
	for _, profile := range []generated.EmailMailboxProviderProfile{"CUSTOM", "TIMEWEB_PASSWORD", "GMAIL_APP_PASSWORD"} {
		value, ok := mailboxSpecificationInput(&generated.EmailMailboxSpecification{ProviderProfile: &profile})
		if !ok || value.GetProviderProfile() == cp.EmailMailboxProviderProfile_EMAIL_MAILBOX_PROVIDER_PROFILE_UNSPECIFIED {
			t.Fatal("explicit provider profile lost on input")
		}
		view, ok := mailboxSpecificationView(value)
		if !ok || view.ProviderProfile == nil || *view.ProviderProfile != profile {
			t.Fatal("explicit provider profile lost on output")
		}
	}
	unknown := generated.EmailMailboxProviderProfile("GMAIL_OAUTH")
	if _, ok := mailboxSpecificationInput(&generated.EmailMailboxSpecification{ProviderProfile: &unknown}); ok {
		t.Fatal("unknown profile accepted")
	}
	if _, ok := mailboxSpecificationView(&cp.EmailMailboxSpecification{ProviderProfile: cp.EmailMailboxProviderProfile(99)}); ok {
		t.Fatal("unknown owner profile accepted")
	}
}
