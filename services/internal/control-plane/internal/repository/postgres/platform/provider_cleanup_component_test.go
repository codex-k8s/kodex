package platform

import (
	"context"
	_ "embed"
	"strings"
	"testing"

	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

//go:embed testdata/sql/provider_cleanup_has_terminal_lease.sql
var queryProviderCleanupFixtureHasTerminalLease string

//go:embed testdata/sql/provider_cleanup_exhaust_attempts.sql
var queryProviderCleanupFixtureExhaustAttempts string

//go:embed testdata/sql/provider_cleanup_replace_completion_descriptor.sql
var queryProviderCleanupReplaceCompletionDescriptor string

// Отдельный запуск cleanup сценария создаёт собственный terminal lease через
// настоящий lifecycle, не полагаясь на порядок других Bootstrap subtests.
func ensureProviderCleanupTerminalLease(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, accountID string) {
	t.Helper()
	var exists bool
	if err := repository.pool.QueryRow(ctx, queryProviderCleanupFixtureHasTerminalLease, accountID).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		return
	}
	seedObservedCatalogFixture(t, ctx, repository)
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "provider-cleanup-terminal-project"}, Payload: command.ProjectInput{Name: "Provider cleanup lifecycle", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatalf("create cleanup terminal project: %v", err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "provider-cleanup-terminal-agent", "Cleanup lease fixture")
	launched, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "provider-cleanup-terminal-run"}, Payload: command.LaunchRunInput{
			ProjectRef: project.Project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Complete cleanup lease fixture."}})
	if err != nil || launched.Run == nil {
		t.Fatalf("launch cleanup terminal fixture: %v", err)
	}
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim",
	}, "runtime-controller")
	claimAndCompleteRun(t, ctx, service, worker, launched.Run.Ref, "provider-cleanup-terminal", false)
}

// Credential leases остаются заняты первичным сценарием; отдельно доказываем,
// что metadata receipt создаёт новую fenced cleanup attempt и не завершает её.
func completeProviderAuthorizationCleanupFixture(t *testing.T, ctx context.Context, repository *Repository, accountRef string) {
	t.Helper()
	const owner = "provider-cleanup-component"
	tasks, err := repository.ClaimProviderCredentialCleanupTasks(ctx, owner, 16)
	if err != nil || len(tasks) != 1 || tasks[0].TargetKind != "AUTHORIZATION_METADATA" || tasks[0].AccountRef != accountRef {
		t.Fatalf("claim authorization metadata: tasks=%#v err=%v", tasks, err)
	}
	parent := tasks[0]
	completion := entity.ProviderAuthorizationCleanupResult{Observation: &entity.ProviderAuthorizationCleanupObservation{
		State: "ABSENT_UNFENCED", Target: parent.Authorization,
	}}
	completion.Observation.Target.Kind = "AUTHORIZATION_ABSENCE"
	if _, err := repository.CompleteProviderCredentialCleanupTask(ctx, parent.Ref, owner, parent.Generation, completion); err != nil {
		t.Fatalf("complete authorization metadata: %v", err)
	}
	tasks, err = repository.ClaimProviderCredentialCleanupTasks(ctx, owner, 16)
	if err != nil || len(tasks) != 1 || tasks[0].TargetKind != "AUTHORIZATION_ABSENCE" || tasks[0].Ref == parent.Ref ||
		tasks[0].Generation <= parent.Generation || tasks[0].Authorization.AuthorizationAttemptRef != parent.Authorization.AuthorizationAttemptRef {
		t.Fatalf("claim exact absence fence successor: tasks=%#v err=%v", tasks, err)
	}
	stale := tasks[0]
	if _, err := repository.FailProviderCredentialCleanupTask(ctx, stale.Ref, owner, stale.Generation, "PROVIDER_CREDENTIAL_CLEANUP_CAS_CHANGED"); err != nil {
		t.Fatalf("record exact no-effect CAS mismatch: %v", err)
	}
	if _, err := repository.FailProviderCredentialCleanupTask(ctx, stale.Ref, owner, stale.Generation, "PROVIDER_CREDENTIAL_CLEANUP_CAS_CHANGED"); err != nil {
		t.Fatalf("replay no-effect CAS mismatch: %v", err)
	}
	tasks, err = repository.ClaimProviderCredentialCleanupTasks(ctx, owner, 16)
	if err != nil || len(tasks) != 1 || tasks[0].TargetKind != "AUTHORIZATION_METADATA" || tasks[0].Ref == parent.Ref || tasks[0].Generation <= stale.Generation {
		t.Fatalf("CAS mismatch did not create fresh metadata generation: %v", err)
	}
	fresh := tasks[0]
	completion.Observation.Target = fresh.Authorization
	completion.Observation.Target.Kind = "AUTHORIZATION_ABSENCE"
	if _, err := repository.CompleteProviderCredentialCleanupTask(ctx, fresh.Ref, owner, fresh.Generation, completion); err != nil {
		t.Fatal(err)
	}
	tasks, err = repository.ClaimProviderCredentialCleanupTasks(ctx, owner, 16)
	if err != nil || len(tasks) != 1 || tasks[0].TargetKind != "AUTHORIZATION_ABSENCE" || tasks[0].Generation <= fresh.Generation {
		t.Fatalf("fresh fence successor: %v", err)
	}
	if _, err := repository.CompleteProviderCredentialCleanupTask(ctx, tasks[0].Ref, owner, tasks[0].Generation,
		entity.ProviderAuthorizationCleanupResult{TerminalReceipt: "authorization-absence-fenced-receipt"}); err != nil {
		t.Fatalf("complete authorization absence fence: %v", err)
	}
}

func testProviderDeletionTerminalReadback(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, actor value.Principal) {
	t.Helper()
	for _, credentialFirst := range []bool{true, false} {
		testProviderProducedCompletionOrder(t, ctx, repository, service, actor, credentialFirst)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateProviderAccount, Principal: actor,
		Mutation: value.Mutation{IdempotencyKey: "provider-terminal-create"}, Payload: command.ProviderAccountInput{DefinitionKey: "openai-codex", Name: "Terminal cleanup fixture"}})
	if err != nil || created.ProviderAccount == nil {
		t.Fatalf("create terminal cleanup account: %v", err)
	}
	account := created.ProviderAccount
	descriptor := entity.ProviderCredentialDescriptor{SecretName: "runtime-provider-terminal-fixture", SecretUID: "61000000-0000-4000-8000-000000000009",
		SecretResourceVersion: "terminal-9", ContentSHA256: strings.Repeat("9", 64)}
	authorized, err := service.Execute(ctx, command.Command{Kind: command.AuthorizeProviderAPIKey, Principal: actor,
		Mutation: value.Mutation{IdempotencyKey: "provider-terminal-authorize", ExpectedVersion: &account.Version},
		Payload: command.ProviderAccountInput{AccountRef: account.Ref, AuthorizationRef: "pauth_terminal_cleanup_fixture",
			AuthorizationMethod: "API_KEY", AuthorizationState: "AUTHORIZED", ExternalAccountMasked: "Terminal fixture", Credential: &descriptor}})
	if err != nil || authorized.ProviderAccount == nil {
		t.Fatalf("authorize terminal cleanup account: %v", err)
	}
	deletionCommand := command.Command{Kind: command.DeleteProviderAccount, Principal: actor,
		Mutation: value.Mutation{IdempotencyKey: "provider-terminal-delete", ExpectedVersion: &authorized.ProviderAccount.Version},
		Payload:  command.ProviderAccountInput{AccountRef: account.Ref}}
	started, err := service.Execute(ctx, deletionCommand)
	if err != nil || started.ProviderAccount == nil || started.ProviderAccount.State != "DELETING" || started.ProviderAccount.Deletion == nil {
		t.Fatalf("start terminal cleanup: %v", err)
	}
	const worker = "provider-cleanup-terminal"
	var credentialTask platformrepo.ProviderCredentialCleanupTask
	for pass := 0; pass < 3; pass++ {
		tasks, err := repository.ClaimProviderCredentialCleanupTasks(ctx, worker, 16)
		if err != nil {
			t.Fatalf("claim terminal cleanup: %v", err)
		}
		for _, task := range tasks {
			if task.AccountRef != account.Ref {
				t.Fatalf("terminal fixture claimed unrelated task %s", task.Ref)
			}
			completion := entity.ProviderAuthorizationCleanupResult{TerminalReceipt: "terminal-fence-" + task.Ref}
			switch task.TargetKind {
			case "CREDENTIAL":
				credentialTask = task
				continue
			case "AUTHORIZATION_METADATA":
				target := task.Authorization
				target.Kind = "AUTHORIZATION_ABSENCE"
				completion = entity.ProviderAuthorizationCleanupResult{Observation: &entity.ProviderAuthorizationCleanupObservation{State: "ABSENT_UNFENCED", Target: target}}
			case "AUTHORIZATION_ABSENCE":
			default:
				t.Fatalf("unexpected terminal cleanup kind %s", task.TargetKind)
			}
			if _, err := repository.CompleteProviderCredentialCleanupTask(ctx, task.Ref, worker, task.Generation, completion); err != nil {
				t.Fatalf("complete terminal authorization cleanup: %v", err)
			}
		}
	}
	if credentialTask.Ref == "" || credentialTask.Credential != descriptor {
		t.Fatal("terminal credential cleanup lost exact descriptor")
	}
	before, err := service.GetProviderAccount(ctx, actor, account.Ref)
	if err != nil || before.State != "DELETING" || before.Deletion == nil || before.Deletion.PendingCleanup != 1 {
		t.Fatalf("account became terminal before credential cleanup: state=%s err=%v", before.State, err)
	}
	if _, err := repository.pool.Exec(ctx, queryProviderCleanupFixtureExhaustAttempts, credentialTask.Ref); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.FailProviderCredentialCleanupTask(ctx, credentialTask.Ref, worker, credentialTask.Generation, "PROVIDER_CREDENTIAL_CLEANUP_TIMEOUT"); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ClaimProviderCredentialCleanupTasks(ctx, worker, 16); err != nil {
		t.Fatal(err)
	}
	failed, err := service.GetProviderAccount(ctx, actor, account.Ref)
	if err != nil || failed.Deletion == nil || failed.Deletion.State != "FAILED" || !contains(failed.NextActions, "DELETE") {
		t.Fatalf("failed cleanup recovery readback: %v", err)
	}
	retryDelete := deletionCommand
	retryDelete.Mutation = value.Mutation{IdempotencyKey: "provider-terminal-retry", ExpectedVersion: &failed.Version}
	if _, err := service.Execute(ctx, retryDelete); err != nil {
		t.Fatalf("explicit deletion recovery: %v", err)
	}
	tasks, err := repository.ClaimProviderCredentialCleanupTasks(ctx, worker, 16)
	if err != nil || len(tasks) != 1 || tasks[0].Ref == credentialTask.Ref || tasks[0].Generation <= credentialTask.Generation || tasks[0].Credential != credentialTask.Credential || tasks[0].Recovery == nil || credentialTask.Recovery == nil || *tasks[0].Recovery != *credentialTask.Recovery {
		t.Fatalf("cleanup recovery lost immutable target/origin: %v", err)
	}
	if _, err := repository.CompleteProviderCredentialCleanupTask(ctx, credentialTask.Ref, worker, credentialTask.Generation, entity.ProviderAuthorizationCleanupResult{TerminalReceipt: "late-old-claim"}); err == nil {
		t.Fatal("superseded cleanup accepted late completion")
	}
	credentialTask = tasks[0]
	produced := descriptor
	produced.SecretUID = "61000000-0000-4000-8000-000000000010"
	produced.SecretResourceVersion = "replacement-10"
	produced.ContentSHA256 = strings.Repeat("a", 64)
	if _, err := repository.CompleteProviderCredentialCleanupTask(ctx, credentialTask.Ref, worker, credentialTask.Generation,
		entity.ProviderAuthorizationCleanupResult{TerminalReceipt: "terminal-credential-deleted", ProducedCredential: &produced}); err != nil {
		t.Fatalf("complete terminal credential cleanup: %v", err)
	}
	orphans, err := repository.ClaimProviderCredentialCleanupTasks(ctx, worker, 16)
	if err != nil || len(orphans) != 1 || orphans[0].TargetKind != "CREDENTIAL" || orphans[0].Credential != produced {
		t.Fatalf("recovered receipt lost produced replacement: %v", err)
	}
	if _, err := repository.CompleteProviderCredentialCleanupTask(ctx, orphans[0].Ref, worker, orphans[0].Generation, entity.ProviderAuthorizationCleanupResult{TerminalReceipt: "replacement-fenced"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ClaimProviderCredentialCleanupTasks(ctx, worker, 16); err != nil {
		t.Fatalf("advance terminal deletion: %v", err)
	}
	terminal, err := service.GetProviderAccount(ctx, actor, account.Ref)
	if err != nil || terminal.State != "DELETED" || terminal.Enabled || terminal.Ready || terminal.Authorization != nil ||
		terminal.Deletion == nil || terminal.Deletion.State != "DELETED" || terminal.Deletion.PendingCleanup != 0 || terminal.Deletion.CompletedAt == nil {
		t.Fatalf("terminal deletion readback is incomplete: state=%s err=%v", terminal.State, err)
	}
	items, _, _, err := service.ListProviderAccounts(ctx, actor, query.Filter{Page: query.Page{Size: 100}})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Ref == account.Ref {
			t.Fatal("default provider list exposed deleted account")
		}
	}
	replayed, err := service.Execute(ctx, deletionCommand)
	if err != nil || replayed.ProviderAccount == nil || replayed.ProviderAccount.State != "DELETED" || replayed.ProviderAccount.Version != terminal.Version {
		t.Fatalf("delete receipt did not retain fresh terminal readback: %v", err)
	}
}

func testProviderProducedCompletionOrder(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, actor value.Principal, credentialFirst bool) {
	t.Helper()
	key, uid := "authorization-first", "61000000-0000-4000-8000-000000000021"
	if credentialFirst {
		key, uid = "credential-first", "61000000-0000-4000-8000-000000000022"
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateProviderAccount, Principal: actor, Mutation: value.Mutation{IdempotencyKey: key + "-create"}, Payload: command.ProviderAccountInput{DefinitionKey: "openai-codex", Name: key}})
	if err != nil || created.ProviderAccount == nil {
		t.Fatal(err)
	}
	account := created.ProviderAccount
	descriptor := entity.ProviderCredentialDescriptor{SecretName: "runtime-provider-" + key, SecretUID: uid, SecretResourceVersion: "1", ContentSHA256: strings.Repeat("b", 64)}
	authorized, err := service.Execute(ctx, command.Command{Kind: command.AuthorizeProviderAPIKey, Principal: actor, Mutation: value.Mutation{IdempotencyKey: key + "-authorize", ExpectedVersion: &account.Version}, Payload: command.ProviderAccountInput{AccountRef: account.Ref, AuthorizationRef: "pauth_" + key, AuthorizationMethod: "API_KEY", AuthorizationState: "AUTHORIZED", ExternalAccountMasked: "Order fixture", Credential: &descriptor}})
	if err != nil || authorized.ProviderAccount == nil {
		t.Fatal(err)
	}
	_, err = service.Execute(ctx, command.Command{Kind: command.DeleteProviderAccount, Principal: actor, Mutation: value.Mutation{IdempotencyKey: key + "-delete", ExpectedVersion: &authorized.ProviderAccount.Version}, Payload: command.ProviderAccountInput{AccountRef: account.Ref}})
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := repository.ClaimProviderCredentialCleanupTasks(ctx, key, 16)
	if err != nil {
		t.Fatal(err)
	}
	var credential, authorization platformrepo.ProviderCredentialCleanupTask
	for _, task := range tasks {
		if task.AccountRef != account.Ref {
			t.Fatalf("unrelated cleanup in order fixture: %s", task.Ref)
		}
		if task.TargetKind == "CREDENTIAL" {
			credential = task
			continue
		}
		if task.TargetKind != "AUTHORIZATION_METADATA" {
			t.Fatalf("unexpected cleanup kind: %s", task.TargetKind)
		}
		target := task.Authorization
		target.Kind = "AUTHORIZATION_ABSENCE"
		if _, err := repository.CompleteProviderCredentialCleanupTask(ctx, task.Ref, key, task.Generation, entity.ProviderAuthorizationCleanupResult{Observation: &entity.ProviderAuthorizationCleanupObservation{State: "ABSENT_UNFENCED", Target: target}}); err != nil {
			t.Fatal(err)
		}
	}
	tasks, err = repository.ClaimProviderCredentialCleanupTasks(ctx, key, 16)
	if err != nil || len(tasks) != 1 || tasks[0].TargetKind != "AUTHORIZATION_ABSENCE" {
		t.Fatalf("authorization successor: %v", err)
	}
	authorization = tasks[0]
	if credential.Ref == "" {
		t.Fatal("missing credential cleanup")
	}
	completeCredential := func() {
		if _, err := repository.CompleteProviderCredentialCleanupTask(ctx, credential.Ref, key, credential.Generation, entity.ProviderAuthorizationCleanupResult{TerminalReceipt: key + "-credential"}); err != nil {
			t.Fatal(err)
		}
	}
	if credentialFirst {
		completeCredential()
	}
	completion := entity.ProviderAuthorizationCleanupResult{TerminalReceipt: key + "-authorization", ProducedCredential: &descriptor}
	foreign := completion
	foreignDescriptor := descriptor
	foreignDescriptor.SecretUID = "10000000-0000-4000-8000-000000000001"
	foreign.ProducedCredential = &foreignDescriptor
	if _, err := repository.CompleteProviderCredentialCleanupTask(ctx, authorization.Ref, key, authorization.Generation, foreign); err == nil {
		t.Fatal("foreign produced credential accepted")
	}
	if credentialFirst {
		if _, err := repository.pool.Exec(ctx, queryProviderCleanupReplaceCompletionDescriptor, credential.Ref, `{}`); err == nil {
			t.Fatal("terminal cleanup proof was mutable")
		}
		if _, err := repository.CompleteProviderCredentialCleanupTask(ctx, authorization.Ref, key, authorization.Generation, entity.ProviderAuthorizationCleanupResult{ProducedCredential: &descriptor}); err == nil {
			t.Fatal("incomplete broker terminal receipt accepted")
		}
	} else {
		if _, err := repository.CompleteProviderCredentialCleanupTask(ctx, credential.Ref, key, credential.Generation, entity.ProviderAuthorizationCleanupResult{TerminalReceipt: "self-cycle", ProducedCredential: &descriptor}); err == nil {
			t.Fatal("self cleanup cycle accepted")
		}
	}
	if _, err := repository.CompleteProviderCredentialCleanupTask(ctx, authorization.Ref, key, authorization.Generation, completion); err != nil {
		t.Fatalf("legal produced receipt after sibling cleanup (%s): %v", key, err)
	}
	// Потерянный ACK повторяет тот же immutable broker результат.
	if _, err := repository.CompleteProviderCredentialCleanupTask(ctx, authorization.Ref, key, authorization.Generation, completion); err != nil {
		t.Fatalf("produced receipt replay: %v", err)
	}
	if !credentialFirst {
		completeCredential()
	}
	if _, err := repository.ClaimProviderCredentialCleanupTasks(ctx, key, 16); err != nil {
		t.Fatal(err)
	}
	terminal, err := service.GetProviderAccount(ctx, actor, account.Ref)
	if err != nil || terminal.State != "DELETED" || terminal.Deletion == nil || terminal.Deletion.PendingCleanup != 0 {
		t.Fatalf("completion order blocked deletion (%s): %s %v", key, terminal.State, err)
	}
}
