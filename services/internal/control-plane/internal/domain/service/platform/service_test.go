package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// readinessRepository injects only the owned repository readiness result while embedding
// unsupported platform operations.
// readinessRepository задаёт только результат готовности собственного репозитория со встраиванием
// неподдерживаемых операций платформы.
type readinessRepository struct {
	platformrepo.Repository
	err error
}

// Ready returns the configured local repository readiness error without checking optional
// materialization adapters.
// Ready возвращает заданную локальную ошибку готовности репозитория без проверки необязательных
// адаптеров материализации.
func (repository *readinessRepository) Ready(context.Context) error {
	return repository.err
}

// unavailableProviderMaterializer records dependency-check attempts and supplies inert
// materialization methods for proving readiness does not depend on this optional port.
// unavailableProviderMaterializer записывает попытки проверки зависимости и предоставляет
// неактивные методы материализации для доказательства независимости readiness от необязательного
// порта.
type unavailableProviderMaterializer struct {
	checkCalls int
}

// Check counts a materializer readiness probe and returns a fixed synthetic unavailable error.
// Check подсчитывает проверку готовности materializer и возвращает фиксированную синтетическую
// ошибку недоступности.
func (materializer *unavailableProviderMaterializer) Check(context.Context) error {
	materializer.checkCalls++
	return errors.New("provider materializer is unavailable")
}

// StartDeviceAuthorization returns empty synthetic device metadata for the unused materialization
// branch of the readiness fixture.
// StartDeviceAuthorization возвращает пустые синтетические метаданные устройства для
// неиспользуемой ветки материализации оснастки readiness.
func (*unavailableProviderMaterializer) StartDeviceAuthorization(context.Context, string, string) (ProviderDeviceAuthorizationMaterialization, error) {
	return ProviderDeviceAuthorizationMaterialization{}, nil
}

// ObserveDeviceAuthorization returns an empty synthetic observation for the unused polling branch
// of the readiness fixture.
// ObserveDeviceAuthorization возвращает пустое синтетическое наблюдение для неиспользуемой ветки
// опроса оснастки readiness.
func (*unavailableProviderMaterializer) ObserveDeviceAuthorization(context.Context, string) (ProviderAuthorizationObservation, error) {
	return ProviderAuthorizationObservation{}, nil
}

// MaterializeAPIKey returns empty synthetic descriptor and masked label for the unused
// key-materialization branch of the readiness fixture.
// MaterializeAPIKey возвращает пустые синтетические дескриптор и скрытую метку для неиспользуемой
// ветки материализации ключа оснастки readiness.
func (*unavailableProviderMaterializer) MaterializeAPIKey(context.Context, string, string, []byte) (entity.ProviderCredentialDescriptor, string, error) {
	return entity.ProviderCredentialDescriptor{}, "", nil
}

// Discard succeeds without effects for the unused compensation branch of the readiness fixture.
// Discard завершается успешно без эффектов для неиспользуемой ветки компенсации оснастки
// readiness.
func (*unavailableProviderMaterializer) Discard(context.Context, ProviderMaterializationDiscard) error {
	return nil
}

// TestReadyDoesNotDependOnProviderCredentialMaterializer checks owned-state readiness succeeds
// while the unavailable optional materializer receives no readiness probe.
// TestReadyDoesNotDependOnProviderCredentialMaterializer проверяет успешную готовность
// собственного состояния без проверок недоступного необязательного materializer.
func TestReadyDoesNotDependOnProviderCredentialMaterializer(t *testing.T) {
	t.Parallel()

	materializer := &unavailableProviderMaterializer{}
	service, err := New(
		&readinessRepository{},
		WithProviderCredentialMaterializer(materializer),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Ready(context.Background()); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}
	if materializer.checkCalls != 0 {
		t.Fatalf("provider materializer readiness calls = %d, want 0", materializer.checkCalls)
	}
}

// TestPreserveRequestContext checks canceled request context takes precedence over an operation
// error while an active request preserves the original domain error.
// TestPreserveRequestContext проверяет приоритет отменённого контекста запроса над ошибкой
// операции и сохранение исходной доменной ошибки активным запросом.
func TestPreserveRequestContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := preserveRequestContext(ctx, errs.ErrUnavailable); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request error = %v", err)
	}
	if err := preserveRequestContext(t.Context(), errs.ErrUnavailable); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatalf("active request error = %v", err)
	}
}

// TestReadyReturnsOwnedRepositoryFailure checks the exact owned repository readiness failure is
// preserved by the service.
// TestReadyReturnsOwnedRepositoryFailure проверяет сохранение сервисом точной ошибки готовности
// собственного репозитория.
func TestReadyReturnsOwnedRepositoryFailure(t *testing.T) {
	t.Parallel()

	want := errors.New("platform repository is unavailable")
	service, err := New(&readinessRepository{err: want})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Ready(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Ready() error = %v, want %v", err, want)
	}
}

// promptPreviewRepository provides synthetic principal and permission resolution for full-prompt
// authentication freshness tests without external services.
// promptPreviewRepository предоставляет синтетическое разрешение principal и permission для тестов
// свежести аутентификации полного prompt без внешних сервисов.
type promptPreviewRepository struct {
	platformrepo.Repository
}

// ResolvePrincipal returns the supplied synthetic preview principal unchanged so tests isolate
// authentication freshness rather than identity lookup.
// ResolvePrincipal возвращает переданный синтетический principal предпросмотра без изменений для
// отдельной проверки свежести аутентификации вместо поиска identity.
func (*promptPreviewRepository) ResolvePrincipal(_ context.Context, principal value.Principal) (value.Principal, error) {
	return principal, nil
}

// QueryEffectiveAccess allows the first requested permission in a synthetic decision while
// preserving the supplied evaluation timestamp.
// QueryEffectiveAccess разрешает первое запрошенное permission в синтетическом решении с
// сохранением переданного времени оценки.
func (*promptPreviewRepository) QueryEffectiveAccess(
	_ context.Context,
	_ value.Principal,
	_ string,
	_ entity.AccessScope,
	permissions []string,
	evaluatedAt time.Time,
) (entity.EffectiveAccess, error) {
	return entity.EffectiveAccess{Decisions: []entity.EffectiveAccessDecision{{
		PermissionKey: permissions[0], Allowed: true,
	}}, EvaluatedAt: evaluatedAt}, nil
}

// TestPreviewPromptTemplateFullRequiresFreshAuthentication checks full previews reject stale
// authentication or missing ACR/AMR and succeed only with fresh interactive proof in the local
// permission fixture.
// TestPreviewPromptTemplateFullRequiresFreshAuthentication проверяет отказ полному предпросмотру
// при устаревшей аутентификации либо отсутствии ACR и AMR и успех со свежим interactive proof в
// локальной оснастке permissions.
func TestPreviewPromptTemplateFullRequiresFreshAuthentication(t *testing.T) {
	t.Parallel()
	service, err := New(&promptPreviewRepository{})
	if err != nil {
		t.Fatal(err)
	}
	principal := value.Principal{
		ActorID: "usr_preview", AuthorityTenant: "org_preview", Permission: "platform.query.prompt-templates.preview",
		CorrelationRef: "cor_preview", CallerWorkload: "control-api-gateway", CredentialRevision: 1,
		CredentialAuthenticatedAt: time.Now().Add(-10 * time.Minute), CredentialACR: "urn:kodex:acr:interactive",
		CredentialAMR: []string{"pwd"},
	}
	if _, err := service.PreviewPromptTemplate(t.Context(), principal, "{{ .user.ref }}", "SYNTHETIC", "", true); !errors.Is(err, errs.ErrFreshAuthenticationRequired) {
		t.Fatalf("stale full prompt preview error = %v", err)
	}
	principal.CredentialAuthenticatedAt = time.Now()
	principal.CredentialACR = ""
	if _, err := service.PreviewPromptTemplate(t.Context(), principal, "{{ .user.ref }}", "SYNTHETIC", "", true); !errors.Is(err, errs.ErrFreshAuthenticationRequired) {
		t.Fatalf("full prompt preview without ACR error = %v", err)
	}
	principal.CredentialACR = "urn:kodex:acr:interactive"
	principal.CredentialAMR = nil
	if _, err := service.PreviewPromptTemplate(t.Context(), principal, "{{ .user.ref }}", "SYNTHETIC", "", true); !errors.Is(err, errs.ErrFreshAuthenticationRequired) {
		t.Fatalf("full prompt preview without AMR error = %v", err)
	}
	principal.CredentialAMR = []string{"pwd"}
	materialized, err := service.PreviewPromptTemplate(t.Context(), principal, "{{ .user.ref }}", "SYNTHETIC", "", true)
	if err != nil || materialized.Prompt == "" {
		t.Fatalf("fresh full prompt preview = %#v, err=%v", materialized, err)
	}
}
