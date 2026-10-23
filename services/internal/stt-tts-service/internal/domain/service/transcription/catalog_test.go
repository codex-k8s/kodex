package transcription

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/sttapi/modelprofile"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/integration/audio/ffmpeg"
)

// catalogProvider counts local catalog reads separately from model and network calls.
// catalogProvider считает локальные чтения каталога отдельно от вызовов модели и сети.
type catalogProvider struct {
	fakeProvider
	catalog      modelprofile.Catalog
	catalogCalls int
}

// Catalog returns the synthetic adapter snapshot and records that this path was used.
// Catalog возвращает снимок тестового адаптера и отмечает использование этого пути.
func (provider *catalogProvider) Catalog() modelprofile.Catalog {
	provider.catalogCalls++
	return provider.catalog
}

// TestGetModelCatalogBeforeConfigurationHasNoRemoteEffect proves catalog reads
// need no policy or credential and do not confer microphone permission.
// RU: TestGetModelCatalogBeforeConfigurationHasNoRemoteEffect доказывает, что
// чтение каталога не требует политики или реквизита и не даёт права микрофона.
func TestGetModelCatalogBeforeConfigurationHasNoRemoteEffect(t *testing.T) {
	policies, credentials := &fakePolicy{err: errs.ErrPolicyUnavailable}, &fakeCredential{err: errs.ErrCredentialUnavailable}
	provider := &catalogProvider{catalog: modelprofile.OpenAICatalog()}
	service, err := New(policies, credentials, provider, &observed{}, time.Second, ffmpeg.New(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	principal := validInput(time.Now(), nil).Principal
	principal.Permission, principal.ProjectID = value.PermissionManageConfiguration, ""
	catalog, err := service.GetModelCatalog(t.Context(), principal)
	if err != nil || !reflect.DeepEqual(catalog, provider.catalog) || provider.catalogCalls != 1 {
		t.Fatal("каталог adapter недоступен до настройки")
	}
	if policies.calls != 0 || credentials.calls != 0 || provider.calls != 0 || provider.localCalls != 0 || provider.egressCalls != 0 {
		t.Fatal("чтение каталога обратилось к policy, credential или provider")
	}
	if _, err := service.CheckAvailability(t.Context(), principal, "correlation"); !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatal("административное чтение расширило право микрофона")
	}
}

// TestGetModelCatalogRejectsAuthorityExpiryAndCancellation rejects stale,
// wrong-scope or cancelled administration before reading the adapter catalog.
// RU: TestGetModelCatalogRejectsAuthorityExpiryAndCancellation отклоняет
// устаревшее, чужое по области или отменённое администрирование до чтения каталога.
func TestGetModelCatalogRejectsAuthorityExpiryAndCancellation(t *testing.T) {
	provider := &catalogProvider{catalog: modelprofile.OpenAICatalog()}
	service, _ := New(&fakePolicy{}, &fakeCredential{}, provider, &observed{}, time.Second, ffmpeg.New(t.TempDir()))
	for name, mutate := range map[string]func(*value.Principal){
		"speech permission":  func(p *value.Principal) { p.Permission = value.PermissionTranscribe },
		"project":            func(p *value.Principal) { p.ProjectID = "project" },
		"project provenance": func(p *value.Principal) { p.Project.Reference = "project" },
		"actor":              func(p *value.Principal) { p.ActorID = "" },
		"tenant":             func(p *value.Principal) { p.TenantID = "" },
		"source":             func(p *value.Principal) { p.AuthorityDigestSHA256 = "" },
		"expiry":             func(p *value.Principal) { p.ExpiresAt = time.Now().Add(-time.Second) },
	} {
		t.Run(name, func(t *testing.T) {
			principal := validInput(time.Now(), nil).Principal
			principal.Permission, principal.ProjectID = value.PermissionManageConfiguration, ""
			mutate(&principal)
			if _, err := service.GetModelCatalog(t.Context(), principal); !errors.Is(err, errs.ErrPermissionDenied) {
				t.Fatal("неверная authority принята")
			}
		})
	}
	principal := validInput(time.Now(), nil).Principal
	principal.Permission, principal.ProjectID = value.PermissionManageConfiguration, ""
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := service.GetModelCatalog(ctx, principal); !errors.Is(err, context.Canceled) || provider.catalogCalls != 0 {
		t.Fatal("отменённый запрос прочитал каталог")
	}
	provider.catalog = modelprofile.Catalog{}
	if _, err := service.GetModelCatalog(t.Context(), principal); !errors.Is(err, errs.ErrProviderUnavailable) {
		t.Fatal("отсутствующий adapter catalog принят")
	}
}

// TestPolicyProviderTimeoutUsesAdapterLimit accepts the exact adapter maximum
// while rejecting every tested timeout that would outlive its bound.
// RU: TestPolicyProviderTimeoutUsesAdapterLimit принимает точный предел адаптера
// и отклоняет проверяемые сроки, выходящие за эту границу.
func TestPolicyProviderTimeoutUsesAdapterLimit(t *testing.T) {
	policy := validPolicy(time.Now())
	policy.ProviderTimeout = modelprofile.MaximumProviderTimeout
	if err := validatePolicy(policy, time.Now()); err != nil {
		t.Fatal("максимальный timeout adapter отклонён")
	}
	for _, timeout := range []time.Duration{modelprofile.MaximumProviderTimeout + time.Millisecond, time.Minute, 2 * time.Minute} {
		policy.ProviderTimeout = timeout
		if err := validatePolicy(policy, time.Now()); !errors.Is(err, errs.ErrGrantRevoked) {
			t.Fatal("timeout шире adapter policy принят")
		}
	}
}
