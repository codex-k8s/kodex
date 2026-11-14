package platform

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
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
	list                      func(query.Filter) ([]entity.ProviderDefinition, string, error)
}

// ResolvePrincipal preserves the synthetic principal only on the configured success path; rejection prevents any catalog read.
// ResolvePrincipal сохраняет синтетический principal только на настроенном успешном пути; отказ запрещает любое чтение каталога.
func (repository *modelDiscoveryRepository) ResolvePrincipal(_ context.Context, p value.Principal) (value.Principal, error) {
	return p, repository.principalError
}

// ListProviderDefinitions models the existing organization-view owner boundary and returns no account credentials or fabricated models.
// ListProviderDefinitions моделирует существующую owner boundary organization-view без возврата account credentials или выдуманных моделей.
func (repository *modelDiscoveryRepository) ListProviderDefinitions(_ context.Context, _ value.Principal, filter query.Filter) ([]entity.ProviderDefinition, string, error) {
	repository.reads++
	if repository.list != nil {
		return repository.list(filter)
	}
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
	if _, err := service.ListModelDiscovery(t.Context(), principal, query.Filter{}, query.ModelDiscoveryFilter{}); !errors.Is(err, errs.ErrForbidden) || repository.reads != 0 {
		t.Fatal("principal rejection bypassed")
	}
	repository.principalError = nil
	repository.readError = errs.ErrForbidden
	if _, err := service.ListModelDiscovery(t.Context(), principal, query.Filter{}, query.ModelDiscoveryFilter{}); !errors.Is(err, errs.ErrForbidden) || repository.reads != 1 {
		t.Fatal("organization view rejection bypassed")
	}
	repository.readError = nil
	page, err := service.ListModelDiscovery(t.Context(), principal, query.Filter{Query: "glm-5.3"}, query.ModelDiscoveryFilter{ProviderID: "zai-coding-plan"})
	if err != nil || len(page.Items) < 2 {
		t.Fatal("authorized discovery unavailable")
	}
	for _, model := range page.Items {
		if model.Allowed != "UNAVAILABLE" || model.Tested != "NOT_RUN" {
			t.Fatal("metadata invented execution authority")
		}
	}
}

// TestModelDiscoveryReadsCompleteBoundedRegistry exercises more than one provider page, exact budget, overflow and revocation between pages without allowing partial evidence.
// TestModelDiscoveryReadsCompleteBoundedRegistry проверяет несколько provider pages, точный бюджет, переполнение и отзыв между страницами без выдачи частичных evidence.
func TestModelDiscoveryReadsCompleteBoundedRegistry(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		count   int
		failure bool
		revoke  bool
		repeat  bool
	}{
		{name: "wide", count: 201}, {name: "exact limit", count: modelDiscoveryDefinitionLimit},
		{name: "overflow", count: modelDiscoveryDefinitionLimit + 1, failure: true},
		{name: "revoked on second page", count: 201, failure: true, revoke: true},
		{name: "repeated cursor", count: 201, failure: true, repeat: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			repository := &modelDiscoveryRepository{}
			repository.list = func(filter query.Filter) ([]entity.ProviderDefinition, string, error) {
				offset := 0
				if filter.Page.Token != "" {
					var err error
					offset, err = strconv.Atoi(filter.Page.Token)
					if err != nil {
						t.Fatal(err)
					}
				}
				if scenario.revoke && offset > 0 {
					return nil, "", errs.ErrForbidden
				}
				end := min(offset+int(filter.Page.Size), scenario.count)
				rows := []entity.ProviderDefinition{}
				for index := offset; index < end; index++ {
					definition := entity.ProviderDefinition{Key: fmt.Sprintf("provider-%d", index)}
					if index == scenario.count-1 {
						config := runtimecontract.ModelProviderConfiguration{Schema: runtimecontract.ModelProviderConfigurationSchema, ManagedBy: "git", SourceRevision: strings.Repeat("a", 40), Engine: "opencode", EngineVersion: runtimecontract.OpenCodeVersion, ToolMode: "managed_mcp", ProviderID: "zai-coding-plan", BaseURL: "https://api.z.ai/api/coding/paas/v4", API: runtimecontract.ModelAPIChatCompletions, RouteKind: "DIRECT", AddressCIDRs: []string{"192.0.2.1/32"}, Models: []runtimecontract.ModelProviderModel{{ID: "glm-5.3", ContextWindowTokens: 1000000, MaximumOutputTokens: 32768}}}
						definition.ModelProvider, definition.Available, definition.Models = &config, true, []entity.ModelCapability{{ID: "glm-5.3", Available: true}}
					}
					rows = append(rows, definition)
				}
				next := ""
				if end < scenario.count {
					next = strconv.Itoa(end)
				}
				if scenario.repeat && offset > 0 {
					next = filter.Page.Token
				}
				return rows, next, nil
			}
			service, err := New(repository)
			if err != nil {
				t.Fatal(err)
			}
			page, err := service.ListModelDiscovery(t.Context(), testInstructionPrincipal(), query.Filter{Query: "glm-5.3"}, query.ModelDiscoveryFilter{ProviderID: "zai-coding-plan"})
			if scenario.failure {
				if err == nil || len(page.Items) != 0 {
					t.Fatal("incomplete owner registry returned metadata")
				}
				return
			}
			if err != nil || len(page.Items) == 0 || repository.reads < 3 {
				t.Fatal("complete wide owner registry unavailable")
			}
			found := false
			for _, model := range page.Items {
				if model.ModelID == "glm-5.3" {
					found = true
					if !model.RouteConfigured || model.Account != "AVAILABLE" {
						t.Fatal("last provider page lost authoritative route/account evidence")
					}
				}
			}
			if !found {
				t.Fatal("expected discovery model missing")
			}
		})
	}
}
