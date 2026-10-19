package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProjectPurgeRecoveryComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	objects := &preparedObjectsFixture{Store: objectstoragetest.New(), fail: true}
	r, err := New(pool, "openai-codex", "gpt-5", objects)
	if err != nil {
		t.Fatal(err)
	}
	configureComponentArtifactScanner(t, r)
	if err := r.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles", DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureProjectPurgeStorage("https://storage.invalid", "fixture", "artifacts", true); err != nil {
		t.Fatal(err)
	}
	if err := r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.query.projects.purge-recovery.get"}, "control-api-gateway")
	service, err := platformservice.New(r)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "purge-recovery-create"}, Payload: command.ProjectInput{Name: "Purge recovery fixture", Language: "ru"}})
	if err != nil || created.Project == nil {
		t.Fatal("create recovery project", err)
	}
	ref := created.Project.Ref
	var orgID, orgRef, actorID, projectID string
	if err := pool.QueryRow(ctx, `SELECT project.organization_id::text,organization.ref,project.created_by::text,project.id::text FROM control_plane.projects project JOIN control_plane.organizations organization ON organization.id=project.organization_id WHERE project.ref=$1`, ref).Scan(&orgID, &orgRef, &actorID, &projectID); err != nil {
		t.Fatal(err)
	}
	digest := "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	request := preparedContentRequest{Binding: preparedContentBinding{ActorID: actorID, OrganizationID: orgID, OrganizationRef: orgRef, ProjectID: projectID, ProjectRef: ref, SourceProfile: "PROJECT", SourceProfileRef: "agent_recovery_component", SourceProfileVersion: 1, SourceContextDigest: strings.Repeat("a", 64), SourceLeaseRef: "lease_recovery_component", SourceLeaseGeneration: 1, SourceFenceDigest: strings.Repeat("b", 64), SourceRunRef: "run_recovery_component", OperationKey: "operation-recovery", IntentOperation: "propose-assistant-plan", IntentDigest: strings.Repeat("c", 64)}, FileName: "recovery.txt", MediaType: "text/plain", Digest: digest, ScanState: "CLEAN", PreviewState: "AVAILABLE", SizeBytes: 3}
	for index := 0; index < 2; index++ {
		request.Binding.IdempotencyKey = fmt.Sprintf("purge-recovery-intent-%d", index)
		request.Body = strings.NewReader("abc")
		if _, err := r.stagePreparedContent(ctx, request); !errors.Is(err, errs.ErrUnavailable) {
			t.Fatal("uncertain original Put was accepted", err)
		}
		for attempt := 0; attempt < 3; attempt++ {
			var ledgerID string
			var generation int64
			var uncertain, accepted bool
			if err := pool.QueryRow(ctx, `SELECT ledger_id::text,generation,uncertain FROM control_plane.prepared_content_claim('recovery-initial-cleanup',1,30)`).Scan(&ledgerID, &generation, &uncertain); err != nil || !uncertain {
				t.Fatal("claim uncertain original upload", err)
			}
			if err := pool.QueryRow(ctx, `SELECT control_plane.prepared_content_finish($1,'recovery-initial-cleanup',$2,false,'','')`, ledgerID, generation).Scan(&accepted); err != nil || !accepted {
				t.Fatal("record uncertain cleanup failure", err)
			}
		}
	}
	if objects.puts.Load() != 2 {
		t.Fatal("uncertain upload retried Put")
	}
	trashed, err := service.Execute(ctx, command.Command{Kind: command.TrashProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "purge-recovery-trash", ExpectedVersion: &created.Project.Version}, Payload: command.ProjectLifecycleInput{Ref: ref}})
	if err != nil || trashed.Project == nil {
		t.Fatal("trash recovery project", err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.PurgeProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "purge-recovery-purge", ExpectedVersion: &trashed.Project.Version}, Payload: command.ProjectLifecycleInput{Ref: ref}}); err != nil {
		t.Fatal("request project purge", err)
	}
	preview, err := service.GetProjectPurgeRecovery(ctx, owner, ref)
	if err != nil || preview.State != "WAITING_OWNER" || preview.UnknownContentCount != 2 || !preview.CanResolve || !preview.ProjectPresent || preview.ProjectVersion < 1 {
		t.Fatal("missing owner-safe blocked preview", preview, err)
	}
	candidate := ProjectPurgeCandidate{OrganizationID: orgID, OrganizationRef: orgRef, ProjectID: projectID, ProjectRef: ref}
	if _, err := r.ProjectPurgeInventory(ctx, candidate); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("unresolved uncertainty did not block ordinary project purge", err)
	}
	resolve := command.Command{Kind: command.ResolveProjectPurgeRecovery, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "purge-recovery-resolution", ExpectedVersion: &preview.Version}, Payload: command.ProjectPurgeRecoveryInput{Ref: ref, SnapshotDigest: preview.SnapshotDigest, AcknowledgeUnknownOutcome: true}}
	t.Run("owner-scope-and-explicit-acknowledgement", func(t *testing.T) {
		for _, mutate := range []func(*value.Principal){
			func(p *value.Principal) { p.ActorID = "30000000-0000-4000-8000-000000000001" },
			func(p *value.Principal) { p.AuthorityTenant = "30000000-0000-4000-8000-000000000002" },
			func(p *value.Principal) { p.ProjectRef = projectID },
			func(p *value.Principal) { p.CallerWorkload = "runtime-controller" },
		} {
			other := owner
			mutate(&other)
			if _, err := service.GetProjectPurgeRecovery(ctx, other, ref); err == nil {
				t.Fatal("foreign principal read recovery")
			}
			denied := resolve
			denied.Principal = other
			if _, err := service.Execute(ctx, denied); err == nil {
				t.Fatal("foreign principal resolved recovery")
			}
		}
		unconfirmed := resolve
		unconfirmed.Payload = command.ProjectPurgeRecoveryInput{Ref: ref, SnapshotDigest: preview.SnapshotDigest}
		if _, err := service.Execute(ctx, unconfirmed); !errors.Is(err, errs.ErrInvalid) {
			t.Fatal("missing acknowledgement was accepted", err)
		}
		withoutVersion := resolve
		withoutVersion.Mutation.ExpectedVersion = nil
		if _, err := service.Execute(ctx, withoutVersion); !errors.Is(err, errs.ErrInvalid) {
			t.Fatal("missing recovery OCC was accepted", err)
		}
		stale := resolve
		stale.Payload = command.ProjectPurgeRecoveryInput{Ref: ref, SnapshotDigest: strings.Repeat("f", 64), AcknowledgeUnknownOutcome: true}
		if _, err := service.Execute(ctx, stale); !errors.Is(err, errs.ErrVersionMismatch) {
			t.Fatal("stale snapshot was accepted", err)
		}
		fresh, err := service.GetProjectPurgeRecovery(ctx, owner, ref)
		if err != nil || fresh.Version != preview.Version || fresh.SnapshotDigest != preview.SnapshotDigest {
			t.Fatal("rejected attempts mutated recovery", err)
		}
	})
	resolved, err := service.Execute(ctx, resolve)
	if err != nil || resolved.ProjectPurgeRecovery == nil || resolved.ProjectPurgeRecovery.ResolutionRef == "" || resolved.ProjectPurgeRecovery.CanResolve || resolved.ProjectPurgeRecovery.UnknownContentCount != 0 || resolved.ProjectPurgeRecovery.CleanupPendingCount != 2 {
		t.Fatal("resolution did not retain physical cleanup obligations", err)
	}
	replayed, err := service.Execute(ctx, resolve)
	if err != nil || replayed.ProjectPurgeRecovery == nil || replayed.ProjectPurgeRecovery.ResolutionRef != resolved.ProjectPurgeRecovery.ResolutionRef {
		t.Fatal("resolution replay changed receipt", err)
	}
	alteredReplay := resolve
	alteredReplay.Payload = command.ProjectPurgeRecoveryInput{Ref: ref, SnapshotDigest: strings.Repeat("f", 64), AcknowledgeUnknownOutcome: true}
	if _, err := service.Execute(ctx, alteredReplay); !errors.Is(err, errs.ErrIdempotencyReuse) {
		t.Fatal("altered resolution reused prior consent", err)
	}
	stale := resolve
	stale.Mutation.IdempotencyKey = "purge-recovery-stale-resolution"
	if _, err := service.Execute(ctx, stale); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatal("old recovery version was accepted", err)
	}
	var masked bool
	if err := pool.QueryRow(ctx, `SELECT bool_and(state='OWNER_ABANDONED' AND deletion_receipt_digest IS NULL AND owner_resolution IS NOT NULL AND ref IS NULL AND file_name IS NULL AND media_type IS NULL AND digest=$2 AND size_bytes=3 AND object_key IS NOT NULL) FROM control_plane.prepared_content WHERE origin_project_ref=$1`, ref, digest).Scan(&masked); err != nil || !masked {
		t.Fatal("owner resolution forged cleanup or retained user metadata", err)
	}
	claims, err := r.ClaimProjectPurgeRecovery(ctx, "recovery-replica-a", 2)
	if err != nil || len(claims) != 2 {
		t.Fatal("claim durable obligations", err)
	}
	if other, err := r.ClaimProjectPurgeRecovery(ctx, "recovery-replica-b", 2); err != nil || len(other) != 0 {
		t.Fatal("two replicas claimed one obligation", err)
	}
	if err := r.FinishProjectPurgeRecovery(ctx, "recovery-replica-b", claims[0], "ABSENT"); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("foreign claimant completed obligation", err)
	}
	if err := r.FinishProjectPurgeRecovery(ctx, "recovery-replica-a", claims[0], "DELETED"); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("unobserved object was marked deleted", err)
	}
	if err := r.FinishProjectPurgeRecovery(ctx, "recovery-replica-a", claims[0], "ABSENT"); err != nil {
		t.Fatal("record bounded absent observation", err)
	}
	if _, err := r.ProjectPurgeInventory(ctx, candidate); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("project purge ignored active recovery claim", err)
	}
	if err := r.FinishProjectPurgeRecovery(ctx, "recovery-replica-a", claims[1], "ABSENT"); err != nil {
		t.Fatal("close active claim with observation before project detach", err)
	}
	var observing bool
	if err := pool.QueryRow(ctx, `SELECT state='OWNER_ABANDONED' AND recovery_state='OBSERVING' AND deletion_receipt_digest IS NULL AND object_key IS NOT NULL FROM control_plane.prepared_content WHERE id=$1`, claims[0].LedgerID).Scan(&observing); err != nil || !observing {
		t.Fatal("HEAD404 became deletion proof", err)
	}
	candidates, err := r.ListPendingProjectPurges(ctx, 4)
	if err != nil || len(candidates) != 1 || candidates[0].ProjectRef != ref {
		t.Fatal("missing pending project", err)
	}
	inventory, err := r.ProjectPurgeInventory(ctx, candidates[0])
	if err != nil || len(inventory) != 0 {
		t.Fatal("unknown storage obligation leaked into ordinary inventory", err)
	}
	if err := r.FinalizeProjectPurge(ctx, candidates[0], inventory); err != nil {
		t.Fatal("finalize resolved project through owner transaction", err)
	}
	after, err := service.GetProjectPurgeRecovery(ctx, owner, ref)
	if err != nil || after.ProjectPresent || after.ProjectVersion != 0 || after.CleanupPendingCount != 2 || after.ResolutionRef == "" {
		t.Fatal("project deletion lost surviving cleanup", after, err)
	}
	// Только время следующей проверки меняется в изолированной fixture.
	// Resolution, pins, state и deletion receipt остаются серверными.
	adminConfig, err := pgx.ParseConfig(os.Getenv("KODEX_CONTROL_PLANE_TEST_ADMIN_DSN"))
	if err != nil || !strings.HasPrefix(pool.Config().ConnConfig.Database, "kodex_assistant_test_") {
		t.Fatal("invalid isolated observation fixture")
	}
	adminConfig.Database = pool.Config().ConnConfig.Database
	admin, err := pgx.ConnectConfig(ctx, adminConfig)
	if err != nil {
		t.Fatal("connect isolated observation fixture")
	}
	defer admin.Close(ctx)
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for _, statement := range []string{
		`SET LOCAL ROLE control_plane_owner`,
		`INSERT INTO control_plane.project_purge_recovery_context(transaction_id,backend_pid,ledger_id,purpose) VALUES(pg_current_xact_id(),pg_backend_pid(),$1,'FINISH')`,
		`UPDATE control_plane.prepared_content SET recovery_check_after=clock_timestamp()-interval '1 minute' WHERE id=$1`,
		`DELETE FROM control_plane.project_purge_recovery_context WHERE transaction_id=pg_current_xact_id() AND ledger_id=$1`,
	} {
		args := []any{claims[1].LedgerID}
		if strings.HasPrefix(statement, "SET ") {
			args = nil
		}
		if _, err := tx.Exec(ctx, statement, args...); err != nil {
			t.Fatal("advance disposable observation deadline", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	lateClaims, err := r.ClaimProjectPurgeRecovery(ctx, "recovery-replica-a", 1)
	if err != nil || len(lateClaims) != 1 || lateClaims[0].LedgerID != claims[1].LedgerID {
		t.Fatal("claim observation after project deletion", err)
	}
	if err := r.FinishProjectPurgeRecovery(ctx, "recovery-replica-a", claims[1], "ABSENT"); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("old generation completed a fresh claim", err)
	}
	// Это позднее завершение первого provider Put, а не повторный stage/Put агента.
	late := lateClaims[0]
	receipt, err := objects.Store.Put(ctx, objectstorage.PutInput{Key: late.Key, MediaType: "text/plain", Digest: late.Digest, SizeBytes: 3, Body: strings.NewReader("abc")})
	if err != nil {
		t.Fatal(err)
	}
	wrong := receipt
	wrong.Key += "-foreign"
	if err := r.PinProjectPurgeRecovery(ctx, "recovery-replica-a", late, wrong); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("mismatched late bytes were pinned", err)
	}
	if err := r.PinProjectPurgeRecovery(ctx, "recovery-replica-a", late, receipt); err != nil {
		t.Fatal("pin late exact receipt after project delete", err)
	}
	if err := objects.Delete(ctx, late.Key, receipt.VersionID); err != nil {
		t.Fatal(err)
	}
	if _, err := objects.Head(ctx, late.Key, receipt.VersionID); !errors.Is(err, objectstorage.ErrNotFound) {
		t.Fatal("late cleanup was not confirmed", err)
	}
	if err := r.FinishProjectPurgeRecovery(ctx, "recovery-replica-a", late, "DELETED"); err != nil {
		t.Fatal("finish late exact cleanup", err)
	}
	after, err = service.GetProjectPurgeRecovery(ctx, owner, ref)
	if err != nil || after.ProjectPresent || after.CleanupPendingCount != 1 || after.UnknownContentCount != 0 {
		t.Fatal("late cleanup lost remaining observation obligation", after, err)
	}
	if objects.puts.Load() != 2 {
		t.Fatal("recovery repeated original agent Put")
	}
}

func TestProjectPurgeRecoveryAdoptedComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	objects := &preparedObjectsFixture{Store: objectstoragetest.New()}
	r, err := New(pool, "openai-codex", "gpt-5", objects)
	if err != nil {
		t.Fatal(err)
	}
	configureComponentArtifactScanner(t, r)
	if err := r.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles", DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureProjectPurgeStorage("https://storage.invalid", "fixture", "artifacts", true); err != nil {
		t.Fatal(err)
	}
	if err := r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	prepareObservedWarmFixture(t, ctx, r)
	owner := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.assistant.conversations.create"}, "control-api-gateway")
	service, err := platformservice.New(r)
	if err != nil {
		t.Fatal(err)
	}
	assistant, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	warm := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.warm.report"}, "runtime-controller")
	if _, err := service.ReportWarmRuntime(ctx, warm, command.WarmRuntimeInput{WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: assistant.DesiredRuntimeRevision, State: "READY"}); err != nil {
		t.Fatal(err)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "purge-adopted-project"}, Payload: command.ProjectInput{Name: "Adopted purge fixture", Language: "ru"}})
	if err != nil || created.Project == nil {
		t.Fatal("create adopted project", err)
	}
	ref := created.Project.Ref
	conversation, err := service.Execute(ctx, command.Command{Kind: command.CreateAssistantConversation, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "purge-adopted-conversation"}, Payload: command.AssistantConversationInput{AssistantScope: "SYSTEM", ProjectRef: ref}})
	if err != nil || conversation.Conversation == nil {
		t.Fatal("create adopted conversation", err)
	}
	var orgID, orgRef, actorID, projectID string
	if err := pool.QueryRow(ctx, `SELECT project.organization_id::text,organization.ref,project.created_by::text,project.id::text FROM control_plane.projects project JOIN control_plane.organizations organization ON organization.id=project.organization_id WHERE project.ref=$1`, ref).Scan(&orgID, &orgRef, &actorID, &projectID); err != nil {
		t.Fatal(err)
	}
	request := preparedContentRequest{Binding: preparedContentBinding{ActorID: actorID, OrganizationID: orgID, OrganizationRef: orgRef, ProjectID: projectID, ProjectRef: ref, SourceProfile: "SYSTEM", SourceProfileRef: "agent_adopted_recovery", SourceProfileVersion: 1, SourceContextDigest: strings.Repeat("a", 64), SourceLeaseRef: "lease_adopted_recovery", SourceLeaseGeneration: 1, SourceFenceDigest: strings.Repeat("b", 64), SourceRunRef: "run_adopted_recovery", OperationKey: "operation-adopted", IntentOperation: "propose-assistant-plan", IdempotencyKey: "purge-adopted-intent", IntentDigest: strings.Repeat("c", 64)}, FileName: "adopted.txt", MediaType: "text/plain", Digest: "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", ScanState: "CLEAN", PreviewState: "AVAILABLE", SizeBytes: 3, Body: strings.NewReader("abc")}
	staged, err := r.stagePreparedContent(ctx, request)
	if err != nil {
		t.Fatal("stage adopted content", err)
	}
	operations, err := json.Marshal([]map[string]any{{"ref": request.Binding.OperationKey, "parameters": map[string]any{"contentRef": staged.ContentRef, "digest": request.Digest, "sizeBytes": 3}}})
	if err != nil {
		t.Fatal(err)
	}
	var planID string
	if err := pool.QueryRow(ctx, `INSERT INTO control_plane.assistant_plans(ref,organization_id,conversation_ref,summary,operations,state) VALUES ('plan_adopted_recovery',$1,$2,'Synthetic',$3,'DRAFT') RETURNING id::text`, orgID, conversation.Conversation.Ref, operations).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO control_plane.assistant_plan_revisions(ref,organization_id,plan_id,revision,summary,operations,content_digest,created_by_kind,created_by_ref) VALUES ('prev_adopted_recovery',$1,$2,1,'Synthetic',$3,repeat('a',64),'USER','actor_adopted_recovery')`, orgID, planID, operations); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	current := scope{organizationID: orgID, actorID: actorID}
	if err := r.linkPreparedContentTx(ctx, tx, current, staged.LedgerID, planID, 1, request.Binding.OperationKey); err != nil {
		t.Fatal("link adopted content to exact plan", err)
	}
	content, err := r.resolvePreparedContentTx(ctx, tx, current, planID, 1, request.Binding.OperationKey, staged.ContentRef, request.Digest, 3)
	if err != nil {
		t.Fatal("resolve exact staged content", err)
	}
	var artifactID, revisionID string
	if err := tx.QueryRow(ctx, `INSERT INTO control_plane.artifacts(ref,organization_id,project_id,file_name,media_type,size_bytes,digest,source,scan_state,object_receipt_ref,preview_state,revision,created_by) VALUES ($1,$2,$3,'adopted.txt','text/plain',3,$4,'CONTROL_CENTER','CLEAN','obj_adopted_recovery','AVAILABLE',1,$5) RETURNING id::text,revision_id::text`, content.ArtifactRef, orgID, projectID, request.Digest, actorID).Scan(&artifactID, &revisionID); err != nil {
		t.Fatal(err)
	}
	scanJSON, err := artifactAntivirusJSON(content.Prepared.Antivirus)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO control_plane.artifact_content(artifact_id,object_key,object_version,object_etag,digest,size_bytes,antivirus_report) VALUES ($1,$2,$3,$4,$5,3,$6)`, artifactID, content.Prepared.ObjectKey, content.Prepared.ObjectVersion, content.Prepared.ObjectETag, content.Prepared.Digest, scanJSON); err != nil {
		t.Fatal(err)
	}
	if err := r.adoptPreparedContentTx(ctx, tx, current, content.LedgerID, revisionID); err != nil {
		t.Fatal("adopt exact artifact revision", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var original bool
	if err := pool.QueryRow(ctx, `SELECT content.state='ADOPTED' AND content.adopted_revision_id=$2 AND content.deletion_receipt_digest IS NULL AND head.lifecycle_state='ACTIVE' FROM control_plane.prepared_content content JOIN control_plane.artifact_heads head ON head.id=$3 WHERE content.id=$1`, content.LedgerID, revisionID, artifactID).Scan(&original); err != nil || !original {
		t.Fatal("adoption fixture did not create normal active content", err)
	}
	trashed, err := service.Execute(ctx, command.Command{Kind: command.TrashProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "purge-adopted-trash", ExpectedVersion: &created.Project.Version}, Payload: command.ProjectLifecycleInput{Ref: ref}})
	if err != nil || trashed.Project == nil {
		t.Fatal("trash adopted project", err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.PurgeProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "purge-adopted-purge", ExpectedVersion: &trashed.Project.Version}, Payload: command.ProjectLifecycleInput{Ref: ref}}); err != nil {
		t.Fatal("request adopted project purge", err)
	}
	candidate := ProjectPurgeCandidate{OrganizationID: orgID, OrganizationRef: orgRef, ProjectID: projectID, ProjectRef: ref}
	inventory, err := r.ProjectPurgeInventory(ctx, candidate)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range inventory {
		if item.Kind != "OBJECT" {
			continue
		}
		if item.Target == content.Prepared.ObjectKey && item.Version == content.Prepared.ObjectVersion {
			found = true
		}
		if err := objects.Delete(ctx, item.Target, item.Version); err != nil {
			t.Fatal(err)
		}
		if _, err := objects.Head(ctx, item.Target, item.Version); !errors.Is(err, objectstorage.ErrNotFound) {
			t.Fatal("adopted object cleanup was not confirmed", err)
		}
	}
	if !found {
		t.Fatal("project inventory omitted adopted exact object")
	}
	var pending bool
	if err := pool.QueryRow(ctx, `SELECT content.state='ADOPTED' AND content.deletion_receipt_digest IS NULL AND content.object_key IS NOT NULL AND head.lifecycle_state<>'PURGED' FROM control_plane.prepared_content content JOIN control_plane.artifact_heads head ON head.id=$2 WHERE content.id=$1`, content.LedgerID, artifactID).Scan(&pending); err != nil || !pending {
		t.Fatal("fixture forged early head purge or ledger scrub", err)
	}
	if err := r.FinalizeProjectPurge(ctx, candidate, inventory); err != nil {
		t.Fatal("ordinary finalization failed on ADOPTED ledger", err)
	}
	var scrubbed bool
	if err := pool.QueryRow(ctx, `SELECT state='ADOPTED' AND project_id IS NULL AND target_artifact_id IS NULL AND plan_id IS NULL AND plan_revision_id IS NULL AND adopted_revision_id IS NULL AND adopted_revision_ref IS NOT NULL AND deletion_receipt_digest ~ '^[a-f0-9]{64}$' AND ref IS NULL AND file_name IS NULL AND media_type IS NULL AND digest IS NULL AND size_bytes IS NULL AND object_key IS NULL AND object_version IS NULL AND object_etag IS NULL AND origin_project_ref=$2 AND origin_plan_ref='plan_adopted_recovery' FROM control_plane.prepared_content WHERE id=$1`, content.LedgerID, ref).Scan(&scrubbed); err != nil || !scrubbed {
		t.Fatal("ADOPTED ledger was not scrubbed before revision deletion", err)
	}
	var removed bool
	if err := pool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM control_plane.projects WHERE id=$1) AND NOT EXISTS(SELECT 1 FROM control_plane.artifact_heads WHERE id=$2) AND NOT EXISTS(SELECT 1 FROM control_plane.artifact_revisions WHERE id=$3) AND EXISTS(SELECT 1 FROM control_plane.project_purge_receipts WHERE project_id=$1 AND state='DONE')`, projectID, artifactID, revisionID).Scan(&removed); err != nil || !removed {
		t.Fatal("adopted project finalization did not finish", err)
	}
	preview, err := service.GetProjectPurgeRecovery(ctx, owner, ref)
	if err != nil || preview.ProjectPresent || preview.State != "NONE" || preview.UnknownContentCount != 0 || preview.CleanupPendingCount != 0 {
		t.Fatal("clean adopted purge fabricated recovery obligations", preview, err)
	}
	if err := r.FinalizeProjectPurge(ctx, candidate, inventory); err != nil {
		t.Fatal("adopted purge replay changed terminal outcome", err)
	}
}
