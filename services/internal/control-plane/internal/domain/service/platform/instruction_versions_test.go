package platform

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// instructionVersionsRepository records the requested agent reference and injects a synthetic
// authorized agent or boundary error for instruction history tests.
// instructionVersionsRepository записывает запрошенную ссылку агента и задаёт синтетического
// разрешённого агента либо ошибку границы для тестов истории инструкций.
type instructionVersionsRepository struct {
	platformrepo.Repository
	agent  entity.Agent
	err    error
	gotRef string
}

// ResolvePrincipal returns the supplied synthetic instruction-history principal unchanged without
// performing an external identity lookup.
// ResolvePrincipal возвращает переданный синтетический principal истории инструкций без изменений
// и внешнего поиска identity.
func (repository *instructionVersionsRepository) ResolvePrincipal(_ context.Context, principal value.Principal) (value.Principal, error) {
	return principal, nil
}

// GetAgent records the exact agent reference and returns the configured fixture agent and error
// without reading live state.
// GetAgent записывает точную ссылку агента и возвращает заданные тестовые агента и ошибку без
// чтения живого состояния.
func (repository *instructionVersionsRepository) GetAgent(_ context.Context, _ value.Principal, ref string) (entity.Agent, error) {
	repository.gotRef = ref
	return repository.agent, repository.err
}

// testInstructionPrincipal builds a synthetic agent-read principal with explicit owner tenant,
// workload, correlation and credential revision.
// testInstructionPrincipal создаёт синтетический principal чтения агента с явными tenant
// владельца, workload, correlation и ревизией реквизитов.
func testInstructionPrincipal() value.Principal {
	return value.Principal{
		ActorID: "actor-id", AuthorityTenant: "organization-id", Permission: "agents.read",
		CorrelationRef: "correlation-id", CallerWorkload: "control-api-gateway", CredentialRevision: 1,
	}
}

// TestListAgentInstructionVersionsUsesAuthorizedAgentReadAndPaginates checks history is read
// through the exact authorized agent and that two-page slicing preserves order and terminal
// cursor.
// TestListAgentInstructionVersionsUsesAuthorizedAgentReadAndPaginates проверяет чтение истории
// через точного разрешённого агента и сохранение порядка и terminal-курсора при разбиении на две
// страницы.
func TestListAgentInstructionVersionsUsesAuthorizedAgentReadAndPaginates(t *testing.T) {
	t.Parallel()
	repository := &instructionVersionsRepository{agent: entity.Agent{PublishedInstructionVersions: []entity.InstructionVersion{
		{Ref: "ins_3", VersionNumber: 3, State: "PUBLISHED"},
		{Ref: "ins_2", VersionNumber: 2, State: "PUBLISHED"},
		{Ref: "ins_1", VersionNumber: 1, State: "PUBLISHED"},
	}}}
	service, err := New(repository)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	first, next, err := service.ListAgentInstructionVersions(context.Background(), testInstructionPrincipal(), "agt_owner", query.Page{Size: 2})
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	if repository.gotRef != "agt_owner" || !reflect.DeepEqual([]string{first[0].Ref, first[1].Ref}, []string{"ins_3", "ins_2"}) || next != "2" {
		t.Fatalf("unexpected first page: ref=%q items=%#v next=%q", repository.gotRef, first, next)
	}
	second, next, err := service.ListAgentInstructionVersions(context.Background(), testInstructionPrincipal(), "agt_owner", query.Page{Size: 2, Token: next})
	if err != nil || len(second) != 1 || second[0].Ref != "ins_1" || next != "" {
		t.Fatalf("unexpected second page: items=%#v next=%q err=%v", second, next, err)
	}
}

// TestListAgentInstructionVersionsRejectsInvalidCursorAndPreservesBoundaryErrors checks malformed
// history cursors fail as invalid and authoritative hidden/missing-agent errors remain not found.
// TestListAgentInstructionVersionsRejectsInvalidCursorAndPreservesBoundaryErrors проверяет отказ
// invalid неверному курсору истории и сохранение not found для авторитетной ошибки скрытого либо
// отсутствующего агента.
func TestListAgentInstructionVersionsRejectsInvalidCursorAndPreservesBoundaryErrors(t *testing.T) {
	t.Parallel()
	repository := &instructionVersionsRepository{agent: entity.Agent{PublishedInstructionVersions: []entity.InstructionVersion{{Ref: "ins_1", VersionNumber: 1}}}}
	service, _ := New(repository)
	if _, _, err := service.ListAgentInstructionVersions(context.Background(), testInstructionPrincipal(), "agt_owner", query.Page{Token: "invalid"}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("invalid cursor error = %v", err)
	}
	repository.err = errs.ErrNotFound
	if _, _, err := service.ListAgentInstructionVersions(context.Background(), testInstructionPrincipal(), "agt_foreign", query.Page{}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("agent boundary error = %v", err)
	}
}
