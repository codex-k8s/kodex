package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testConfigurationSourceLifecycle(t *testing.T, ctx context.Context, repository *Repository, service *serviceplatform.Service, owner value.Principal) {
	t.Helper()
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "cfg-source-connection"},
		Payload: command.ConnectionInput{DefinitionKey: "github", Name: "Git source", PublicConfiguration: map[string]any{"owner": "example", "repository": "configuration"}}})
	if err != nil || created.Connection == nil {
		t.Fatalf("source connection: %v", err)
	}
	resolvedOwner, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatalf("source owner principal: %v", err)
	}
	current, err := repository.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatalf("source owner scope: %v", err)
	}
	// Локальная fixture содержит только immutable descriptor, без credential value.
	_, err = repository.pool.Exec(ctx, `WITH credential AS (
 INSERT INTO control_plane.integration_credential_revisions(ref,organization_id,connection_id,revision,secret_ref,secret_uid,secret_resource_version,content_sha256,created_by)
 SELECT 'icred_source_fixture',organization_id,id,1,'kodex-system/source-fixture#token','10000000-0000-4000-8000-000000000001','1',repeat('a',64),$2::uuid
 FROM control_plane.integration_connections WHERE ref=$1 RETURNING id,connection_id)
 UPDATE control_plane.integration_connections connection SET credential_revision_id=credential.id,state='CONNECTED',masked_credentials_state='CONFIGURED'
 FROM credential WHERE connection.id=credential.connection_id`, created.Connection.Ref, current.actorID)
	if err != nil {
		t.Fatalf("source credential fixture: %v", err)
	}
	connection, err := service.GetIntegrationConnection(ctx, owner, created.Connection.Ref)
	if err != nil {
		t.Fatalf("source connection read: %v", err)
	}
	draft, err := service.Execute(ctx, command.Command{Kind: command.CreateIntegrationDefinition, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "cfg-source-draft"},
		Payload: command.ManagedConfigurationInput{Name: "Source package", ContentFormat: "JSON", Content: string(asJSON(repository.integrationDefinitions["synthetic"]))}})
	if err != nil || draft.ManagedConfiguration == nil {
		t.Fatalf("source draft: %v", err)
	}
	version := draft.ManagedConfiguration.Version
	configure := command.Command{Kind: command.ConfigureIntegrationDefinitionGitSource, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "cfg-source-configure", ExpectedVersion: &version},
		Payload: command.ManagedConfigurationGitSourceInput{ConfigurationRef: draft.ManagedConfiguration.Ref, ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version,
			RepositoryRef: "example/configuration", RefName: "main", Path: "integration.json", ContentFormat: "JSON"}}
	configured, err := service.Execute(ctx, configure)
	if err != nil || configured.ManagedConfiguration == nil || configured.ManagedConfiguration.GitSource == nil {
		t.Fatalf("configure source: %v", err)
	}
	worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.runtime.integrations.tests.claim"}, "integration-gateway")
	worker.Permission = "platform.configuration-sources.work.claim"
	work, err := service.ClaimConfigurationSourceWork(ctx, worker, "source-fixture", 1)
	if err != nil || len(work) != 1 {
		t.Fatalf("claim source count=%d: %v", len(work), err)
	}
	if work[0].ConfigurationRef != draft.ManagedConfiguration.Ref || len(work[0].DefinitionPackage) == 0 {
		t.Fatal("source snapshot lost package/owner pin")
	}
	content := asJSON(repository.integrationDefinitions["synthetic"])
	digest := sha256.Sum256(content)
	completion := port.ConfigurationSourceCompletion{Lease: work[0].Lease, CommitSHA: strings.Repeat("a", 40), ContentSHA256: hex.EncodeToString(digest[:]), Content: content, Ancestry: "INITIAL"}
	worker.Permission = "platform.configuration-sources.work.complete"
	accepted, err := service.CompleteConfigurationSourceWork(ctx, worker, completion)
	if err != nil || accepted.State != "READY" || accepted.AcceptedRevisionRef == "" || accepted.SyncedAt == nil {
		t.Fatalf("accept source: %+v %v", accepted, err)
	}
	replay, err := service.CompleteConfigurationSourceWork(ctx, worker, completion)
	if err != nil || replay.Version != accepted.Version {
		t.Fatalf("replay source: %v", err)
	}
	changed := completion
	changed.CommitSHA = strings.Repeat("b", 40)
	if _, err := service.CompleteConfigurationSourceWork(ctx, worker, changed); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("changed receipt accepted: %v", err)
	}
	read, history, _, _, err := service.ListManagedConfigurationHistory(ctx, owner, draft.ManagedConfiguration.Ref, query.Page{Size: 20})
	if err != nil || read.GitSource == nil || read.GitSource.AcceptedRevisionRef != accepted.AcceptedRevisionRef || len(history) != 2 {
		t.Fatalf("source history count=%d: %v", len(history), err)
	}
	testConfigurationWriteBackLifecycle(t, ctx, repository, service, owner, worker, read, string(content))
	connection, err = service.GetIntegrationConnection(ctx, owner, connection.Ref)
	if err != nil {
		t.Fatalf("connection after writeback recovery: %v", err)
	}
	if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.managed_configuration_git_sources SET next_refresh_at=clock_timestamp()-interval '1 second' WHERE ref=$1`, accepted.Ref); err != nil {
		t.Fatal(err)
	}
	worker.Permission = "platform.configuration-sources.work.claim"
	work, err = service.ClaimConfigurationSourceWork(ctx, worker, "source-fixture", 1)
	if err != nil || len(work) != 1 || work[0].PreviousCommitSHA != completion.CommitSHA {
		t.Fatalf("periodic refresh: %v", err)
	}
	unchanged := completion
	unchanged.Lease = work[0].Lease
	unchanged.Ancestry = "UNCHANGED"
	worker.Permission = "platform.configuration-sources.work.complete"
	if result, err := service.CompleteConfigurationSourceWork(ctx, worker, unchanged); err != nil || result.AcceptedRevisionRef != accepted.AcceptedRevisionRef {
		t.Fatalf("unchanged source republished: %v", err)
	}
	read, history, _, _, err = service.ListManagedConfigurationHistory(ctx, owner, draft.ManagedConfiguration.Ref, query.Page{Size: 20})
	if err != nil || len(history) != 2 {
		t.Fatalf("unchanged source history: %v", err)
	}
	version = read.Version
	refresh, err := service.Execute(ctx, command.Command{Kind: command.RefreshIntegrationDefinitionGitSource, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "cfg-source-refresh", ExpectedVersion: &version}, Payload: command.ManagedConfigurationGitSourceInput{ConfigurationRef: read.Ref}})
	if err != nil || refresh.ManagedConfiguration.GitSource.State != "QUEUED" {
		t.Fatalf("explicit source refresh: %v", err)
	}
	worker.Permission = "platform.configuration-sources.work.claim"
	work, err = service.ClaimConfigurationSourceWork(ctx, worker, "source-fixture", 1)
	if err != nil || len(work) != 1 {
		t.Fatalf("claim retry source: %v", err)
	}
	oldLease := work[0].Lease
	worker.Permission = "platform.configuration-sources.work.fail"
	failed, err := service.FailConfigurationSourceWork(ctx, worker, oldLease, "UNAVAILABLE")
	if err != nil || failed.State != "QUEUED" || failed.FailureCode != "" {
		t.Fatalf("source bounded retry: %+v %v", failed, err)
	}
	if replay, err := service.FailConfigurationSourceWork(ctx, worker, oldLease, "UNAVAILABLE"); err != nil || replay.Version != failed.Version {
		t.Fatalf("source failure replay: %v", err)
	}
	worker.Permission = "platform.configuration-sources.work.claim"
	work, err = service.ClaimConfigurationSourceWork(ctx, worker, "source-fixture", 1)
	if err != nil || len(work) != 1 || work[0].Lease.Attempt != 2 || work[0].Lease.ClaimGeneration <= oldLease.ClaimGeneration {
		t.Fatalf("source retry fence: %v", err)
	}
	worker.Permission = "platform.configuration-sources.work.renew"
	if _, err := service.RenewConfigurationSourceWork(ctx, worker, oldLease); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("old source lease renewed: %v", err)
	}
	read, _, _, _, err = service.ListManagedConfigurationHistory(ctx, owner, draft.ManagedConfiguration.Ref, query.Page{Size: 20})
	if err != nil {
		t.Fatal(err)
	}
	version = read.Version
	detached, err := service.Execute(ctx, command.Command{Kind: command.DetachGitManagedConfiguration, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "cfg-source-detach", ExpectedVersion: &version},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: read.Ref}})
	if err != nil || detached.ManagedConfiguration.GitSource.State != "DETACHED" || detached.ManagedConfiguration.ManagedBy != "UI" {
		t.Fatalf("detach source: %v", err)
	}
	worker.Permission = "platform.configuration-sources.work.complete"
	unchanged.Lease = work[0].Lease
	if _, err := service.CompleteConfigurationSourceWork(ctx, worker, unchanged); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("detached claimed source completed: %v", err)
	}
	worker.Permission = "platform.configuration-sources.work.claim"
	work, err = service.ClaimConfigurationSourceWork(ctx, worker, "source-fixture", 1)
	if err != nil || len(work) != 0 {
		t.Fatalf("detached source claimed: %v", err)
	}
	testRoleImageSourcePublication(t, ctx, repository, service, owner, worker, connection.Ref, connection.Version)
}

func testRoleImageSourcePublication(t *testing.T, ctx context.Context, repository *Repository, service *serviceplatform.Service, owner, worker value.Principal, connectionRef string, connectionVersion int64) {
	t.Helper()
	catalog, _ := promotionComponentCatalog(t)
	repository.ConfigureRoleImageCatalog(catalog.Resolve)
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "source-role-project"}, Payload: command.ProjectInput{Name: "Role source project", Language: "ru"}})
	if err != nil || project.Project == nil {
		t.Fatalf("source role project: %v", err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "source-role-agent", "Source image role")
	content := asJSON(map[string]any{"name": "Git role recipe", "roleImage": map[string]any{"roleDefinitionRef": agent.RoleDefinitionRef, "environment": map[string]any{"environmentKey": "promotion"}}})
	draft, err := service.Execute(ctx, command.Command{Kind: command.CreateRoleImageRevisionDraft, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "source-role-draft"}, Payload: command.ManagedConfigurationInput{ProjectRef: project.Project.Ref, Name: "Git role recipe", ContentFormat: "JSON", Content: string(content)}})
	if err != nil || draft.ManagedConfiguration == nil {
		t.Fatalf("source role draft: %v", err)
	}
	version := draft.ManagedConfiguration.Version
	_, err = service.Execute(ctx, command.Command{Kind: command.ConfigureRoleImageGitSource, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "source-role-configure", ExpectedVersion: &version}, Payload: command.ManagedConfigurationGitSourceInput{ConfigurationRef: draft.ManagedConfiguration.Ref, ConnectionRef: connectionRef, ExpectedConnectionVersion: connectionVersion, RepositoryRef: "example/configuration", RefName: "main", Path: "role.json", ContentFormat: "JSON"}})
	if err != nil {
		t.Fatalf("configure role source: %v", err)
	}
	worker.Permission = "platform.configuration-sources.work.claim"
	work, err := service.ClaimConfigurationSourceWork(ctx, worker, "source-role-fixture", 1)
	if err != nil || len(work) != 1 {
		t.Fatalf("role source claim: %v", err)
	}
	digest := sha256.Sum256(content)
	worker.Permission = "platform.configuration-sources.work.complete"
	completion := port.ConfigurationSourceCompletion{Lease: work[0].Lease, CommitSHA: strings.Repeat("c", 40), ContentSHA256: hex.EncodeToString(digest[:]), Content: content, Ancestry: "INITIAL"}
	accepted, err := service.CompleteConfigurationSourceWork(ctx, worker, completion)
	if err != nil || accepted.State != "READY" {
		t.Fatalf("role source accept: %+v %v", accepted, err)
	}
	var count int
	var state string
	err = repository.pool.QueryRow(ctx, `SELECT count(*)::integer,min(build.stage) FROM control_plane.managed_role_image_builds mapping
 JOIN control_plane.image_builds build ON build.id=mapping.build_id
 JOIN control_plane.managed_configuration_revisions revision ON revision.id=mapping.configuration_revision_id WHERE revision.ref=$1`, accepted.AcceptedRevisionRef).Scan(&count, &state)
	if err != nil || count != 1 || state != "QUEUED" {
		t.Fatalf("role source did not enqueue exact build: %d %s %v", count, state, err)
	}
	if replay, err := service.CompleteConfigurationSourceWork(ctx, worker, completion); err != nil || replay.AcceptedRevisionRef != accepted.AcceptedRevisionRef {
		t.Fatalf("role source replay: %v", err)
	}
}
