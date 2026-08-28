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

type instructionVersionsRepository struct {
	platformrepo.Repository
	agent  entity.Agent
	err    error
	gotRef string
}

func (repository *instructionVersionsRepository) ResolvePrincipal(_ context.Context, principal value.Principal) (value.Principal, error) {
	return principal, nil
}

func (repository *instructionVersionsRepository) GetAgent(_ context.Context, _ value.Principal, ref string) (entity.Agent, error) {
	repository.gotRef = ref
	return repository.agent, repository.err
}

func testInstructionPrincipal() value.Principal {
	return value.Principal{
		ActorID: "actor-id", AuthorityTenant: "organization-id", Permission: "agents.read",
		CorrelationRef: "correlation-id", CallerWorkload: "control-api-gateway", CredentialRevision: 1,
	}
}

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
