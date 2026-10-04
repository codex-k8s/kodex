package platform

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testProviderAccountConcurrency(t *testing.T, ctx context.Context, repository *Repository) {
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.provider-accounts.concurrency.set",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateProviderAccount, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "concurrency-create"},
		Payload:  command.ProviderAccountInput{Name: "Concurrency fixture", DefinitionKey: "openai-codex"}})
	if err != nil || created.ProviderAccount == nil {
		t.Fatalf("create concurrency fixture: %v", err)
	}
	account := *created.ProviderAccount
	if account.MaximumConcurrentExecutions != 10 {
		t.Fatalf("default concurrency: %d", account.MaximumConcurrentExecutions)
	}
	makeCommand := func(limit int32, key string, version int64) command.Command {
		return command.Command{Kind: command.SetProviderAccountConcurrency, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &version},
			Payload:  command.ProviderAccountInput{AccountRef: account.Ref, MaximumConcurrentExecutions: limit}}
	}
	for _, limit := range []int32{0, 257, -1} {
		_, err := service.Execute(ctx, makeCommand(limit, "concurrency-invalid-"+strconv.Itoa(int(limit)), account.Version))
		if !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("invalid limit %d: %v", limit, err)
		}
	}
	for _, limit := range []int32{1, 256, 10} {
		input := makeCommand(limit, "concurrency-limit-"+strconv.Itoa(int(limit)), account.Version)
		changed, err := service.Execute(ctx, input)
		if err != nil || changed.ProviderAccount == nil {
			t.Fatalf("change limit %d: %v", limit, err)
		}
		updated := *changed.ProviderAccount
		if updated.MaximumConcurrentExecutions != limit || updated.Version != account.Version+1 || updated.State != account.State || updated.Enabled != account.Enabled {
			t.Fatal("concurrency mutation changed lifecycle or lost version")
		}
		replay, err := service.Execute(ctx, input)
		if err != nil || replay.ProviderAccount == nil || replay.ProviderAccount.Version != updated.Version || replay.ProviderAccount.MaximumConcurrentExecutions != limit {
			t.Fatalf("exact concurrency receipt: %v", err)
		}
		changedPayload := input
		changedPayload.Payload = command.ProviderAccountInput{AccountRef: account.Ref, MaximumConcurrentExecutions: limit + 1}
		if _, err := service.Execute(ctx, changedPayload); !errors.Is(err, errs.ErrIdempotencyReuse) {
			t.Fatalf("changed receipt payload: %v", err)
		}
		if _, err := service.Execute(ctx, makeCommand(5, "concurrency-stale-"+strconv.Itoa(int(limit)), account.Version)); !errors.Is(err, errs.ErrVersionMismatch) {
			t.Fatalf("stale concurrency version: %v", err)
		}
		account = updated
	}
	read, err := service.GetProviderAccountWithUsage(ctx, owner, account.Ref, nil)
	if err != nil || read.MaximumConcurrentExecutions != 10 || read.Usage == nil || read.Usage.MaximumConcurrentExecutions != 10 {
		t.Fatalf("authoritative account/usage readback: %v", err)
	}
	page, _, _, err := service.ListProviderAccounts(ctx, owner, query.Filter{Query: "Concurrency fixture", Page: query.Page{Size: 10}})
	if err != nil || len(page) != 1 || page[0].MaximumConcurrentExecutions != 10 {
		t.Fatalf("account list concurrency: %v", err)
	}

	// Делегированное manage не превращает обычного участника в администратора аккаунтов.
	candidate := platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000007720", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Concurrency operator", CallerWorkload: "control-api-gateway", Operation: "platform.query.provider-accounts.list"}
	if _, err := repository.ResolveProofAuthority(ctx, candidate); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("unbound operator: %v", err)
	}
	subjects, _, err := service.ListAccessSubjects(ctx, owner, query.Filter{Query: candidate.ExternalDisplayName}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatalf("operator registration: %v", err)
	}
	role := createRoleImageAccessRole(t, ctx, service, owner, "concurrency-operator-role", "Concurrency operator", []string{"provider.account.view", "provider.account.manage"}, []string{"RESOURCE_KIND"})
	createRoleImageAccessBinding(t, ctx, service, owner, "concurrency-operator-binding", subjects[0].Ref, role.CurrentVersion.Ref, entity.AccessScope{Kind: "RESOURCE_KIND", ResourceKind: "PROVIDER_ACCOUNT"})
	reader := resolvedTestPrincipal(t, ctx, repository, candidate, "control-api-gateway")
	input := makeCommand(5, "concurrency-operator-denied", account.Version)
	input.Principal = reader
	if _, err := service.Execute(ctx, input); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("operator mutated owner settings: %v", err)
	}
	stale := account.Version + 999
	input.Mutation.ExpectedVersion = &stale
	if _, err := service.Execute(ctx, input); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("version validation preceded operator authority: %v", err)
	}
	read, err = service.GetProviderAccountWithUsage(ctx, reader, account.Ref, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range read.NextActions {
		if action == "EDIT" {
			t.Fatal("operator sees forbidden edit action")
		}
	}
	input = makeCommand(5, "concurrency-unknown-account", account.Version)
	input.Payload = command.ProviderAccountInput{AccountRef: "pacc_missing0001", MaximumConcurrentExecutions: 5}
	if _, err := service.Execute(ctx, input); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("unknown account: %v", err)
	}
}
