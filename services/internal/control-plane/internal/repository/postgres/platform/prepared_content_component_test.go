package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

type preparedObjectsFixture struct {
	*objectstoragetest.Store
	puts             atomic.Int64
	fail             bool
	entered, release chan struct{}
}

func TestPreparedContentBindingComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var graph string
	if err := pool.QueryRow(ctx, `SELECT substring(pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) FROM 'IF v_nodes <> [0-9]+ OR v_node_fingerprint <> ''[a-f0-9]{32}'' THEN') || E'\n' || substring(pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) FROM 'IF v_edges <> [0-9]+ OR v_edge_fingerprint <> ''[a-f0-9]{32}'' THEN')`).Scan(&graph); err != nil {
		t.Fatal(err)
	}
	t.Log(graph)
	objects := &preparedObjectsFixture{Store: objectstoragetest.New()}
	repository, err := New(pool, "openai-codex", "gpt-5", objects)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles", DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	prepareObservedWarmFixture(t, ctx, repository)
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.assistant.conversations.create"}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	assistant, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	warm := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.warm.report"}, "runtime-controller")
	if _, err := service.ReportWarmRuntime(ctx, warm, command.WarmRuntimeInput{WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: assistant.DesiredRuntimeRevision, State: "READY"}); err != nil {
		t.Fatal(err)
	}
	projectResult, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "ledger-binding-project"}, Payload: command.ProjectInput{Name: "Ledger binding", Language: "ru"}})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := service.Execute(ctx, command.Command{Kind: command.CreateAssistantConversation, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "ledger-binding-conversation"}, Payload: command.AssistantConversationInput{AssistantScope: "SYSTEM"}})
	if err != nil {
		t.Fatal(err)
	}
	var orgID, actorID, projectID string
	if err := pool.QueryRow(ctx, `SELECT project.organization_id::text,project.created_by::text,project.id::text FROM control_plane.projects project WHERE ref=$1`, projectResult.Project.Ref).Scan(&orgID, &actorID, &projectID); err != nil {
		t.Fatal(err)
	}
	var orgRef string
	if err := pool.QueryRow(ctx, `SELECT ref FROM control_plane.organizations WHERE id=$1`, orgID).Scan(&orgRef); err != nil {
		t.Fatal(err)
	}
	binding := preparedContentBinding{ActorID: actorID, OrganizationID: orgID, OrganizationRef: orgRef, ProjectID: projectID, ProjectRef: projectResult.Project.Ref, SourceProfile: "SYSTEM", SourceProfileRef: "agent_binding_component", SourceProfileVersion: 1,
		SourceContextDigest: strings.Repeat("a", 64), SourceLeaseRef: "lease_binding_component", SourceLeaseGeneration: 1, SourceFenceDigest: strings.Repeat("b", 64), SourceRunRef: "run_binding_component", OperationKey: "operation-binding", IntentOperation: "propose-assistant-plan", IdempotencyKey: "binding-intent", IntentDigest: strings.Repeat("c", 64)}
	request := preparedContentRequest{Binding: binding, FileName: "file.txt", MediaType: "text/plain", Digest: "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", ScanState: "CLEAN", PreviewState: "AVAILABLE", SizeBytes: 3, Body: strings.NewReader("abc")}
	staged, err := repository.stagePreparedContent(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	operations, _ := json.Marshal([]map[string]any{{"ref": binding.OperationKey, "parameters": map[string]any{"contentRef": staged.ContentRef, "digest": request.Digest, "sizeBytes": 3}}})
	var planID string
	if err := pool.QueryRow(ctx, `INSERT INTO control_plane.assistant_plans(ref,organization_id,conversation_ref,summary,operations,state) VALUES ('plan_binding_component',$1,$2,'Synthetic',$3,'DRAFT') RETURNING id::text`, orgID, conversation.Conversation.Ref, operations).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	insertRevision := func(number int64) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO control_plane.assistant_plan_revisions(ref,organization_id,plan_id,revision,summary,operations,content_digest,created_by_kind,created_by_ref) VALUES ('prev_binding_'||($4::bigint)::text,$1,$2,$4::bigint,'Synthetic',$3,repeat('a',64),'USER','actor_binding_component')`, orgID, planID, operations, number); err != nil {
			t.Fatal(err)
		}
	}
	current := scope{organizationID: orgID, actorID: actorID}
	insertRevision(1)
	for revision := int64(1); revision <= 2; revision++ {
		if revision == 2 {
			if _, err := pool.Exec(ctx, `UPDATE control_plane.assistant_plans SET current_revision=2 WHERE id=$1`, planID); err != nil {
				t.Fatal(err)
			}
			insertRevision(2)
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := repository.linkPreparedContentTx(ctx, tx, current, staged.LedgerID, planID, revision, binding.OperationKey); err != nil {
			tx.Rollback(ctx)
			t.Fatal(err)
		}
		read, err := repository.readPreparedContentBindingTx(ctx, tx, current, planID, revision, binding.OperationKey, staged.ContentRef, request.Digest, 3)
		if err != nil || read != binding {
			tx.Rollback(ctx)
			t.Fatal("stored origin mismatch", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var bindingCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.prepared_content_bindings WHERE ledger_id=$1`, staged.LedgerID).Scan(&bindingCount); err != nil || bindingCount != 2 {
		t.Fatal("binding history was overwritten", bindingCount, err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := repository.resolvePreparedContentTx(ctx, tx, current, planID, 1, binding.OperationKey, staged.ContentRef, request.Digest, 3); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("old immutable plan revision resolved as current", err)
	}
	content, err := repository.resolvePreparedContentTx(ctx, tx, current, planID, 2, binding.OperationKey, staged.ContentRef, request.Digest, 3)
	if err != nil {
		t.Fatal(err)
	}
	var artifactID, revisionID string
	if err := tx.QueryRow(ctx, `INSERT INTO control_plane.artifacts(ref,organization_id,project_id,file_name,media_type,size_bytes,digest,source,scan_state,object_receipt_ref,preview_state,revision,created_by) VALUES ($1,$2,$3,'file.txt','text/plain',3,$4,'CONTROL_CENTER','CLEAN','obj_binding_component','AVAILABLE',1,$5) RETURNING id::text,revision_id::text`, content.ArtifactRef, orgID, projectID, request.Digest, actorID).Scan(&artifactID, &revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO control_plane.artifact_content(artifact_id,object_key,object_version,object_etag,digest,size_bytes) VALUES ($1,$2,$3,$4,$5,3)`, artifactID, content.Prepared.ObjectKey, content.Prepared.ObjectVersion, content.Prepared.ObjectETag, content.Prepared.Digest); err != nil {
		t.Fatal(err)
	}
	if err := repository.adoptPreparedContentTx(ctx, tx, current, content.LedgerID, revisionID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.prepared_content SET available_until=clock_timestamp()-interval '1 minute' WHERE id=$1`, content.LedgerID); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `SELECT ledger_id FROM control_plane.prepared_content_claim('component-cleanup',100,30)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("adopted content claimed for cleanup")
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.prepared_content SET state='CLEANED' WHERE id=$1`, content.LedgerID); err == nil {
		t.Fatal("ADOPTED flag was reversible")
	}
	t.Run("terminal-revision-purge-scrubs-all-content-metadata", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `DELETE FROM control_plane.artifact_revisions WHERE id=$1`, revisionID); err == nil {
			t.Fatal("active immutable revision was deleted")
		}
		if _, err := pool.Exec(ctx, `UPDATE control_plane.prepared_content SET ref=NULL,file_name=NULL,media_type=NULL,digest=NULL,size_bytes=NULL,scan_state=NULL,preview_state=NULL,object_key=NULL,object_version=NULL,object_etag=NULL,adopted_revision_id=NULL,deletion_receipt_digest=repeat('a',64) WHERE id=$1`, content.LedgerID); err == nil {
			t.Fatal("nonterminal ledger scrub was accepted")
		}
		if _, err := pool.Exec(ctx, `UPDATE control_plane.artifact_heads SET lifecycle_state='PURGED',current_revision_id=NULL,purged_at=clock_timestamp() WHERE id=$1`, artifactID); err == nil {
			t.Fatal("purge accepted retained receipt")
		}
		if err := repository.objects.Delete(ctx, content.Prepared.ObjectKey, content.Prepared.ObjectVersion); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.objects.Head(ctx, content.Prepared.ObjectKey, content.Prepared.ObjectVersion); !errors.Is(err, objectstorage.ErrNotFound) {
			t.Fatal("object deletion was not confirmed", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE control_plane.artifact_heads SET lifecycle_state='PURGE_PENDING',deleted_at=clock_timestamp(),purge_after=clock_timestamp(),retention_claim_owner='component-artifact-purge',retention_claim_generation=1,retention_claim_expires_at=clock_timestamp()+interval '1 minute' WHERE id=$1`, artifactID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM control_plane.artifact_revision_content WHERE revision_id=$1 AND object_key=$2 AND object_version=$3 AND object_etag=$4 AND digest=$5 AND size_bytes=3`, revisionID, content.Prepared.ObjectKey, content.Prepared.ObjectVersion, content.Prepared.ObjectETag, content.Prepared.Digest); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE control_plane.artifact_heads SET lifecycle_state='PURGED',current_revision_id=NULL,retention_claim_owner=NULL,retention_claim_expires_at=NULL,retention_claim_generation=0,purged_at=clock_timestamp() WHERE id=$1`, artifactID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM control_plane.artifact_revisions WHERE id=$1`, revisionID); err == nil {
			t.Fatal("ledger scrub accepted missing terminal fence")
		}
		if _, err := pool.Exec(ctx, `UPDATE control_plane.artifact_heads SET retention_claim_generation=1 WHERE id=$1`, artifactID); err != nil {
			t.Fatal(err)
		}
		spoof, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := spoof.Exec(ctx, `CREATE TEMP TABLE prepared_revision_spoof(id uuid,ref text,artifact_id uuid) ON COMMIT DROP`); err != nil {
			spoof.Rollback(ctx)
			t.Fatal(err)
		}
		if _, err := spoof.Exec(ctx, `INSERT INTO prepared_revision_spoof SELECT id,ref,artifact_id FROM control_plane.artifact_revisions WHERE id=$1`, revisionID); err != nil {
			spoof.Rollback(ctx)
			t.Fatal(err)
		}
		if _, err := spoof.Exec(ctx, `CREATE TRIGGER prepared_revision_spoof BEFORE DELETE ON prepared_revision_spoof FOR EACH ROW EXECUTE FUNCTION control_plane.scrub_prepared_content_revision()`); err != nil {
			spoof.Rollback(ctx)
			t.Fatal(err)
		}
		if _, err := spoof.Exec(ctx, `DELETE FROM prepared_revision_spoof`); err == nil || !strings.Contains(err.Error(), "prepared content scrub trigger target is invalid") {
			spoof.Rollback(ctx)
			t.Fatal("temporary trigger forged owner revision purge", err)
		}
		spoof.Rollback(ctx)
		if _, err := pool.Exec(ctx, `DELETE FROM control_plane.artifact_revisions WHERE id=$1`, revisionID); err != nil {
			t.Fatal("terminal revision purge failed", err)
		}
		var masked, preserved bool
		if err := pool.QueryRow(ctx, `SELECT state='ADOPTED' AND adopted_revision_id IS NULL AND deletion_receipt_digest ~ '^[a-f0-9]{64}$' AND ref IS NULL AND file_name IS NULL AND media_type IS NULL AND digest IS NULL AND size_bytes IS NULL AND scan_state IS NULL AND preview_state IS NULL AND object_key IS NULL AND object_version IS NULL AND object_etag IS NULL,
          adopted_revision_ref IS NOT NULL AND intent_digest=$2 AND request_digest=$3 AND origin_plan_ref='plan_binding_component'
          FROM control_plane.prepared_content WHERE id=$1`, content.LedgerID, request.Binding.IntentDigest, preparedContentRequestDigest(request)).Scan(&masked, &preserved); err != nil || !masked || !preserved {
			t.Fatal("terminal ledger mask lost metadata or origin", masked, preserved, err)
		}
		check, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repository.resolvePreparedContentTx(ctx, check, current, planID, 2, binding.OperationKey, content.ContentRef, request.Digest, 3); !errors.Is(err, errs.ErrConflict) {
			check.Rollback(ctx)
			t.Fatal("commitment resolved purged content", err)
		}
		if _, err := repository.readPreparedContentBindingTx(ctx, check, current, planID, 2, binding.OperationKey, content.ContentRef, request.Digest, 3); !errors.Is(err, errs.ErrConflict) {
			check.Rollback(ctx)
			t.Fatal("masked origin restored purged content", err)
		}
		if err := repository.adoptPreparedContentTx(ctx, check, current, content.LedgerID, revisionID); !errors.Is(err, errs.ErrConflict) {
			check.Rollback(ctx)
			t.Fatal("purged ledger adopted again", err)
		}
		check.Rollback(ctx)
		puts := objects.puts.Load()
		request.Body = strings.NewReader("abc")
		if _, err := repository.stagePreparedContent(ctx, request); !errors.Is(err, errs.ErrConflict) || objects.puts.Load() != puts {
			t.Fatal("purged intent repeated Put", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE control_plane.prepared_content SET ref=$2,file_name='file.txt',media_type='text/plain',digest=$3,size_bytes=3,scan_state='CLEAN',preview_state='AVAILABLE',object_key=$4,object_version=$5,object_etag=$6,deletion_receipt_digest=NULL WHERE id=$1`, content.LedgerID, content.ContentRef, request.Digest, content.Prepared.ObjectKey, content.Prepared.ObjectVersion, content.Prepared.ObjectETag); err == nil {
			t.Fatal("masked content was restored")
		}
		var unchanged bool
		if err := pool.QueryRow(ctx, `SELECT operations=$2::jsonb FROM control_plane.assistant_plan_revisions WHERE plan_id=$1 AND revision=2`, planID, operations).Scan(&unchanged); err != nil || !unchanged {
			t.Fatal("public native plan receipt was rewritten", err)
		}
	})
	t.Run("expiry-closes-exact-pending-plan", func(t *testing.T) {
		request.Binding.IdempotencyKey = "cleanup-intent"
		request.Body = strings.NewReader("abc")
		pending, err := repository.stagePreparedContent(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		pendingOperations, _ := json.Marshal([]map[string]any{{"ref": binding.OperationKey, "parameters": map[string]any{"contentRef": pending.ContentRef, "digest": request.Digest, "sizeBytes": 3}}})
		var pendingPlanID string
		if err := pool.QueryRow(ctx, `INSERT INTO control_plane.assistant_plans(ref,organization_id,conversation_ref,summary,operations,state) VALUES ('plan_cleanup_component',$1,$2,'Synthetic',$3,'INVALID') RETURNING id::text`, orgID, conversation.Conversation.Ref, pendingOperations).Scan(&pendingPlanID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO control_plane.assistant_plan_revisions(ref,organization_id,plan_id,revision,summary,operations,content_digest,created_by_kind,created_by_ref) VALUES ('prev_cleanup_component',$1,$2,1,'Synthetic',$3,repeat('a',64),'USER','actor_binding_component')`, orgID, pendingPlanID, pendingOperations); err != nil {
			t.Fatal(err)
		}
		link, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := repository.linkPreparedContentTx(ctx, link, current, pending.LedgerID, pendingPlanID, 1, binding.OperationKey); err != nil {
			link.Rollback(ctx)
			t.Fatal(err)
		}
		if err := link.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE control_plane.prepared_content SET available_until=clock_timestamp()-interval '1 minute',writer_deadline=clock_timestamp()-interval '1 minute' WHERE id=$1`, pending.LedgerID); err != nil {
			t.Fatal(err)
		}
		var claimedID string
		var generation int64
		if err := pool.QueryRow(ctx, `SELECT ledger_id::text,generation FROM control_plane.prepared_content_claim('component-exact-cleanup',100,30) WHERE ledger_id=$1`, pending.LedgerID).Scan(&claimedID, &generation); err != nil {
			t.Fatal(err)
		}
		var planState, otherPlanState string
		if err := pool.QueryRow(ctx, `SELECT state FROM control_plane.assistant_plans WHERE id=$1`, pendingPlanID).Scan(&planState); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT state FROM control_plane.assistant_plans WHERE id=$1`, planID).Scan(&otherPlanState); err != nil {
			t.Fatal(err)
		}
		if planState != "STALE" || otherPlanState != "DRAFT" {
			t.Fatal("cleanup closed the wrong plan", planState, otherPlanState)
		}
		adopt, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := repository.adoptPreparedContentTx(ctx, adopt, current, pending.LedgerID, revisionID); !errors.Is(err, errs.ErrConflict) {
			adopt.Rollback(ctx)
			t.Fatal("claimed content adopted", err)
		}
		adopt.Rollback(ctx)
		var accepted bool
		if err := pool.QueryRow(ctx, `SELECT control_plane.prepared_content_record_receipt($1,'component-exact-cleanup',$2,$3,$4,$5,$6,3)`, claimedID, generation, pending.Prepared.ObjectKey, pending.Prepared.ObjectVersion, pending.Prepared.ObjectETag, pending.Prepared.Digest).Scan(&accepted); err != nil || !accepted {
			t.Fatal("receipt fence rejected", err)
		}
		if err := repository.objects.Delete(ctx, pending.Prepared.ObjectKey, pending.Prepared.ObjectVersion); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.objects.Head(ctx, pending.Prepared.ObjectKey, pending.Prepared.ObjectVersion); !errors.Is(err, objectstorage.ErrNotFound) {
			t.Fatal("cleanup was not confirmed", err)
		}
		if err := pool.QueryRow(ctx, `SELECT control_plane.prepared_content_finish($1,'component-exact-cleanup',$2,true,$3,$4)`, claimedID, generation, pending.Prepared.ObjectVersion, pending.Prepared.ObjectETag).Scan(&accepted); err != nil || !accepted {
			t.Fatal("cleanup receipt rejected", err)
		}
		var state string
		if err := pool.QueryRow(ctx, `SELECT state FROM control_plane.prepared_content WHERE id=$1`, pending.LedgerID).Scan(&state); err != nil || state != "CLEANED" {
			t.Fatal("cleanup did not finish", state, err)
		}
		originTx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repository.readPreparedContentBindingTx(ctx, originTx, current, pendingPlanID, 1, binding.OperationKey, pending.ContentRef, request.Digest, 3); !errors.Is(err, errs.ErrConflict) {
			originTx.Rollback(ctx)
			t.Fatal("owner draft restored cleaned content", err)
		}
		if _, err := repository.resolvePreparedContentTx(ctx, originTx, current, pendingPlanID, 1, binding.OperationKey, pending.ContentRef, request.Digest, 3); !errors.Is(err, errs.ErrConflict) {
			originTx.Rollback(ctx)
			t.Fatal("origin read reused cleaned content", err)
		}
		originTx.Rollback(ctx)
		var masked bool
		if err := pool.QueryRow(ctx, `SELECT deletion_receipt_digest IS NOT NULL AND ref IS NULL AND file_name IS NULL AND media_type IS NULL AND digest IS NULL AND size_bytes IS NULL AND scan_state IS NULL AND preview_state IS NULL AND object_key IS NULL AND object_version IS NULL AND object_etag IS NULL FROM control_plane.prepared_content WHERE id=$1`, pending.LedgerID).Scan(&masked); err != nil || !masked {
			t.Fatal("cleanup retained standalone content metadata", masked, err)
		}
	})
	t.Run("terminal-project-purge-retains-history", func(t *testing.T) {
		created, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "ledger-terminal-project"}, Payload: command.ProjectInput{Name: "Terminal ledger", Language: "ru"}})
		if err != nil {
			t.Fatal(err)
		}
		var terminalProjectID string
		if err := pool.QueryRow(ctx, `SELECT id::text FROM control_plane.projects WHERE ref=$1`, created.Project.Ref).Scan(&terminalProjectID); err != nil {
			t.Fatal(err)
		}
		request.Binding.ProjectID, request.Binding.ProjectRef = terminalProjectID, created.Project.Ref
		request.Binding.IdempotencyKey = "terminal-intent"
		request.Body = strings.NewReader("abc")
		terminal, err := repository.stagePreparedContent(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		terminalOperations, _ := json.Marshal([]map[string]any{{"ref": binding.OperationKey, "parameters": map[string]any{"contentRef": terminal.ContentRef, "digest": request.Digest, "sizeBytes": 3}}})
		var terminalPlanID string
		if err := pool.QueryRow(ctx, `INSERT INTO control_plane.assistant_plans(ref,organization_id,conversation_ref,summary,operations,state) VALUES ('plan_terminal_component',$1,$2,'Synthetic',$3,'DRAFT') RETURNING id::text`, orgID, conversation.Conversation.Ref, terminalOperations).Scan(&terminalPlanID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO control_plane.assistant_plan_revisions(ref,organization_id,plan_id,revision,summary,operations,content_digest,created_by_kind,created_by_ref) VALUES ('prev_terminal_component',$1,$2,1,'Synthetic',$3,repeat('a',64),'USER','actor_binding_component')`, orgID, terminalPlanID, terminalOperations); err != nil {
			t.Fatal(err)
		}
		link, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := repository.linkPreparedContentTx(ctx, link, current, terminal.LedgerID, terminalPlanID, 1, binding.OperationKey); err != nil {
			link.Rollback(ctx)
			t.Fatal(err)
		}
		if err := link.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE control_plane.prepared_content SET available_until=clock_timestamp()-interval '1 minute',writer_deadline=clock_timestamp()-interval '1 minute' WHERE id=$1`, terminal.LedgerID); err != nil {
			t.Fatal(err)
		}
		var generation int64
		if err := pool.QueryRow(ctx, `SELECT generation FROM control_plane.prepared_content_claim('component-terminal-cleanup',100,30) WHERE ledger_id=$1`, terminal.LedgerID).Scan(&generation); err != nil {
			t.Fatal(err)
		}
		if err := repository.objects.Delete(ctx, terminal.Prepared.ObjectKey, terminal.Prepared.ObjectVersion); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.objects.Head(ctx, terminal.Prepared.ObjectKey, terminal.Prepared.ObjectVersion); !errors.Is(err, objectstorage.ErrNotFound) {
			t.Fatal("cleanup was not confirmed", err)
		}
		var accepted bool
		if err := pool.QueryRow(ctx, `SELECT control_plane.prepared_content_finish($1,'component-terminal-cleanup',$2,true,$3,$4)`, terminal.LedgerID, generation, terminal.Prepared.ObjectVersion, terminal.Prepared.ObjectETag).Scan(&accepted); err != nil || !accepted {
			t.Fatal("terminal cleanup rejected", err)
		}
		scopedOwner := owner
		scopedOwner.ProjectRef = terminalProjectID
		trashed, err := service.Execute(ctx, command.Command{Kind: command.TrashProject, Principal: scopedOwner, Mutation: value.Mutation{IdempotencyKey: "ledger-terminal-trash", ExpectedVersion: &created.Project.Version}, Payload: command.ProjectLifecycleInput{Ref: created.Project.Ref}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Execute(ctx, command.Command{Kind: command.PurgeProject, Principal: scopedOwner, Mutation: value.Mutation{IdempotencyKey: "ledger-terminal-purge", ExpectedVersion: &trashed.Project.Version}, Payload: command.ProjectLifecycleInput{Ref: created.Project.Ref}}); err != nil {
			t.Fatal(err)
		}
		var inventoryDigest string
		if err := pool.QueryRow(ctx, `SELECT control_plane.project_purge_inventory_digest($1,$2)`, orgID, terminalProjectID).Scan(&inventoryDigest); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE control_plane.project_purge_receipts SET state='OBJECTS_CLEARED',objects_digest=$2,objects_cleared_at=statement_timestamp() WHERE project_id=$1 AND state='PENDING'`, terminalProjectID, inventoryDigest); err != nil {
			t.Fatal(err)
		}
		var deleted int
		if err := pool.QueryRow(ctx, `SELECT control_plane.purge_project_database($1,$2,$3)`, orgID, terminalProjectID, inventoryDigest).Scan(&deleted); err != nil || deleted < 1 {
			t.Fatal("terminal project purge failed", err)
		}
		var state, origin string
		var remainingFKs int
		if err := pool.QueryRow(ctx, `SELECT state,origin_project_ref,(project_id IS NOT NULL)::integer+(plan_id IS NOT NULL)::integer+(plan_revision_id IS NOT NULL)::integer FROM control_plane.prepared_content WHERE id=$1`, terminal.LedgerID).Scan(&state, &origin, &remainingFKs); err != nil || state != "CLEANED" || origin != created.Project.Ref || remainingFKs != 0 {
			t.Fatal("ledger history lost", state, origin, remainingFKs, err)
		}
		var preserved int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.prepared_content_bindings WHERE ledger_id=$1 AND plan_ref='plan_terminal_component' AND plan_revision_ref='prev_terminal_component' AND plan_id IS NULL AND plan_revision_id IS NULL`, terminal.LedgerID).Scan(&preserved); err != nil || preserved != 1 {
			t.Fatal("binding history lost", preserved, err)
		}
		var masked bool
		if err := pool.QueryRow(ctx, `SELECT deletion_receipt_digest IS NOT NULL AND ref IS NULL AND file_name IS NULL AND media_type IS NULL AND digest IS NULL AND size_bytes IS NULL AND scan_state IS NULL AND preview_state IS NULL AND object_key IS NULL AND object_version IS NULL AND object_etag IS NULL FROM control_plane.prepared_content WHERE id=$1`, terminal.LedgerID).Scan(&masked); err != nil || !masked {
			t.Fatal("cleaned terminal ledger metadata survived purge", err)
		}
	})
}

func (store *preparedObjectsFixture) Put(ctx context.Context, input objectstorage.PutInput) (objectstorage.Receipt, error) {
	store.puts.Add(1)
	if store.entered != nil {
		close(store.entered)
		select {
		case <-ctx.Done():
			return objectstorage.Receipt{}, ctx.Err()
		case <-store.release:
		}
	}
	if store.fail {
		return objectstorage.Receipt{}, objectstorage.ErrUnavailable
	}
	return store.Store.Put(ctx, input)
}

func TestPreparedContentLedgerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var org, actor, project string
	for _, item := range []struct {
		sql    string
		target *string
		args   []any
	}{
		{`INSERT INTO control_plane.organizations(ref,name) VALUES ('org_ledger_component','Ledger') RETURNING id::text`, &org, nil},
		{`INSERT INTO control_plane.subjects(ref,organization_id,issuer,external_subject_digest,display_name) VALUES ('sub_ledger_component',$1,'fixture',repeat('a',64),'Synthetic') RETURNING id::text`, &actor, []any{&org}},
		{`INSERT INTO control_plane.projects(ref,organization_id,name,created_by) VALUES ('prj_ledger_component',$1,'Ledger',$2) RETURNING id::text`, &project, []any{&org, &actor}},
	} {
		args := make([]any, len(item.args))
		for i, arg := range item.args {
			args[i] = *arg.(*string)
		}
		if err := pool.QueryRow(ctx, item.sql, args...).Scan(item.target); err != nil {
			t.Fatal(err)
		}
	}
	store := &preparedObjectsFixture{Store: objectstoragetest.New()}
	repository := &Repository{pool: pool, objects: store}
	binding := preparedContentBinding{ActorID: actor, OrganizationID: org, OrganizationRef: "org_ledger_component", ProjectID: project, ProjectRef: "prj_ledger_component",
		SourceProfile: "PROJECT", SourceProfileRef: "agent_ledger_component", SourceProfileVersion: 1, SourceContextDigest: strings.Repeat("a", 64),
		SourceLeaseRef: "lease_ledger_component", SourceLeaseGeneration: 1, SourceFenceDigest: strings.Repeat("b", 64), SourceRunRef: "run_ledger_component",
		OperationKey: "operation-ledger", IntentOperation: "propose-assistant-plan", IdempotencyKey: "ledger-intent", IntentDigest: strings.Repeat("c", 64)}
	request := preparedContentRequest{Binding: binding, FileName: "file.txt", MediaType: "text/plain", Digest: "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", ScanState: "CLEAN", PreviewState: "AVAILABLE", SizeBytes: 3, Body: strings.NewReader("abc")}
	staged, err := repository.stagePreparedContent(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	request.Body = strings.NewReader("abc")
	replay, err := repository.stagePreparedContent(ctx, request)
	if err != nil || replay.ContentRef != staged.ContentRef || store.puts.Load() != 1 {
		t.Fatal("exact replay sent another Put", err)
	}
	request.Binding.SourceLeaseGeneration++
	request.Body = strings.NewReader("abc")
	if _, err := repository.stagePreparedContent(ctx, request); !errors.Is(err, errs.ErrIdempotencyReuse) {
		t.Fatal("altered replay accepted", err)
	}
	request.Binding = binding
	current := scope{organizationID: org, actorID: actor}
	t.Run("unpublished-cannot-resolve", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err := repository.resolvePreparedContentTx(ctx, tx, current, "00000000-0000-4000-8000-000000000001", 1, binding.OperationKey, staged.ContentRef, request.Digest, 3); !errors.Is(err, errs.ErrConflict) {
			t.Fatal("unpublished content resolved", err)
		}
	})
	t.Run("unknown-never-retries-put", func(t *testing.T) {
		request.Binding.IdempotencyKey = "unknown-intent"
		request.Body = strings.NewReader("abc")
		store.fail = true
		if _, err := repository.stagePreparedContent(ctx, request); !errors.Is(err, errs.ErrUnavailable) {
			t.Fatal(err)
		}
		store.fail = false
		request.Body = strings.NewReader("abc")
		if _, err := repository.stagePreparedContent(ctx, request); !errors.Is(err, errs.ErrConflict) {
			t.Fatal(err)
		}
		if store.puts.Load() != 2 {
			t.Fatal("unknown retried Put")
		}
		for attempt := int64(2); attempt <= 4; attempt++ {
			var id, key, version, etag, digest string
			var size, generation int64
			var uncertain bool
			if err := pool.QueryRow(ctx, `SELECT ledger_id::text,object_key,object_version,object_etag,digest,size_bytes,generation,uncertain FROM control_plane.prepared_content_claim('component',1,30)`).Scan(&id, &key, &version, &etag, &digest, &size, &generation, &uncertain); err != nil {
				t.Fatal(err)
			}
			if !uncertain || generation != attempt {
				t.Fatal("unknown claim lost fence")
			}
			var accepted bool
			if err := pool.QueryRow(ctx, `SELECT control_plane.prepared_content_finish($1,'component',$2,true,'unconfirmed-version','unconfirmed-etag')`, id, generation).Scan(&accepted); err == nil {
				t.Fatal("unknown cleanup accepted no persistent positive receipt")
			}
			if err := pool.QueryRow(ctx, `SELECT control_plane.prepared_content_finish($1,'component',$2,false,'','')`, id, generation).Scan(&accepted); err != nil || !accepted {
				t.Fatal(err)
			}
		}
		var state string
		if err := pool.QueryRow(ctx, `SELECT state FROM control_plane.prepared_content WHERE idempotency_key='unknown-intent'`).Scan(&state); err != nil || state != "WAITING_OWNER" {
			t.Fatal("unknown did not retain waiting ledger", state, err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM control_plane.projects WHERE id=$1`, project); err == nil {
			t.Fatal("waiting owner intent did not block project purge")
		}
	})
	t.Run("leased-writer-cannot-clean", func(t *testing.T) {
		request.Binding.IdempotencyKey = "writer-intent"
		request.Body = strings.NewReader("abc")
		store.entered = make(chan struct{})
		store.release = make(chan struct{})
		var wait sync.WaitGroup
		wait.Add(1)
		var stageErr error
		go func() { defer wait.Done(); _, stageErr = repository.stagePreparedContent(ctx, request) }()
		<-store.entered
		rows, err := pool.Query(ctx, `SELECT ledger_id FROM control_plane.prepared_content_claim('parallel-cleanup',100,30)`)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for rows.Next() {
			count++
		}
		rows.Close()
		if rows.Err() != nil {
			t.Fatal(rows.Err())
		}
		if count != 0 {
			t.Fatal("live writer was claimed")
		}
		close(store.release)
		wait.Wait()
		if stageErr != nil {
			t.Fatal(stageErr)
		}
		store.entered = nil
		store.release = nil
	})
	t.Run("cleanup-role-is-narrow", func(t *testing.T) {
		config := pool.Config().Copy()
		config.ConnConfig.User = "artifact_retention_runtime_g1"
		worker, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			t.Fatal(err)
		}
		defer worker.Close()
		rows, err := worker.Query(ctx, `SELECT ledger_id FROM control_plane.prepared_content_claim('worker-role',1,30)`)
		if err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if _, err := worker.Exec(ctx, `UPDATE control_plane.prepared_content SET state='CLEANED'`); err == nil {
			t.Fatal("worker has direct ledger DML")
		}
		if _, err := worker.Exec(ctx, `DELETE FROM control_plane.assistant_plans`); err == nil {
			t.Fatal("worker can delete native plans")
		}
	})
}
