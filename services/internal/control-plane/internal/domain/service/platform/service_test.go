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

type readinessRepository struct {
	platformrepo.Repository
	err error
}

func (repository *readinessRepository) Ready(context.Context) error {
	return repository.err
}

type unavailableProviderMaterializer struct {
	checkCalls int
}

func (materializer *unavailableProviderMaterializer) Check(context.Context) error {
	materializer.checkCalls++
	return errors.New("provider materializer is unavailable")
}

func (*unavailableProviderMaterializer) StartDeviceAuthorization(context.Context, string, string) (ProviderDeviceAuthorizationMaterialization, error) {
	return ProviderDeviceAuthorizationMaterialization{}, nil
}

func (*unavailableProviderMaterializer) ObserveDeviceAuthorization(context.Context, string) (ProviderAuthorizationObservation, error) {
	return ProviderAuthorizationObservation{}, nil
}

func (*unavailableProviderMaterializer) MaterializeAPIKey(context.Context, string, string, []byte) (entity.ProviderCredentialDescriptor, string, error) {
	return entity.ProviderCredentialDescriptor{}, "", nil
}

func (*unavailableProviderMaterializer) Discard(context.Context, ProviderMaterializationDiscard) error {
	return nil
}

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

type promptPreviewRepository struct {
	platformrepo.Repository
}

func (*promptPreviewRepository) ResolvePrincipal(_ context.Context, principal value.Principal) (value.Principal, error) {
	return principal, nil
}

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
