package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// modelDiscoveryRepository supplies explicit principal and organization-view outcomes, recording every authoritative read before static metadata can be returned.
// modelDiscoveryRepository задаёт явные исходы principal и organization-view, записывая каждое авторитетное чтение до выдачи статических metadata.
type modelDiscoveryRepository struct {
	port.Repository
	principalError, readError error
	reads                     int
}

// ResolvePrincipal preserves the synthetic principal only on the configured success path; rejection prevents any catalog read.
// ResolvePrincipal сохраняет синтетический principal только на настроенном успешном пути; отказ запрещает любое чтение каталога.
func (repository *modelDiscoveryRepository) ResolvePrincipal(_ context.Context, p value.Principal) (value.Principal, error) {
	return p, repository.principalError
}

// ListProviderDefinitions models the existing organization-view owner boundary and returns no account credentials or fabricated models.
// ListProviderDefinitions моделирует существующую owner boundary organization-view без возврата account credentials или выдуманных моделей.
func (repository *modelDiscoveryRepository) ListProviderDefinitions(_ context.Context, _ value.Principal, _ query.Filter) ([]entity.ProviderDefinition, string, error) {
	repository.reads++
	return nil, "", repository.readError
}

// TestModelDiscoveryAuthorizationPrecedesMetadata proves unauthenticated and forbidden readers receive no static page, while allowed readers retain all five independent dimensions.
// TestModelDiscoveryAuthorizationPrecedesMetadata доказывает отсутствие статической страницы у неаутентифицированных и запрещённых readers; допущенные readers сохраняют пять независимых измерений.
func TestModelDiscoveryAuthorizationPrecedesMetadata(t *testing.T) {
	repository := &modelDiscoveryRepository{principalError: errs.ErrForbidden}
	service, err := New(repository)
	if err != nil {
		t.Fatal(err)
	}
	principal := testInstructionPrincipal()
	if _, err := service.ListModelDiscovery(t.Context(), principal, query.Filter{}, ""); !errors.Is(err, errs.ErrForbidden) || repository.reads != 0 {
		t.Fatal("principal rejection bypassed")
	}
	repository.principalError = nil
	repository.readError = errs.ErrForbidden
	if _, err := service.ListModelDiscovery(t.Context(), principal, query.Filter{}, ""); !errors.Is(err, errs.ErrForbidden) || repository.reads != 1 {
		t.Fatal("organization view rejection bypassed")
	}
	repository.readError = nil
	page, err := service.ListModelDiscovery(t.Context(), principal, query.Filter{Query: "glm-5.3"}, "zai-coding-plan")
	if err != nil || len(page.Items) < 2 {
		t.Fatal("authorized discovery unavailable")
	}
	for _, model := range page.Items {
		if model.Allowed != "UNAVAILABLE" || model.Tested != "NOT_RUN" {
			t.Fatal("metadata invented execution authority")
		}
	}
}
