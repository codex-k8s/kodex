package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// assertPersonalInvocationCredentialPin qualifies immutable owner/credential lineage in rollback
// fixtures, without a provider, JWT, runtime grant admission or call-start claim. The real helper
// receives only a stored invocation ref; a second user's binding is never selected as fallback.
// assertPersonalInvocationCredentialPin проверяет неизменяемую owner/credential lineage внутри
// откатываемых fixtures без provider, JWT, runtime grant admission и допуска начала вызова.
// Настоящий helper получает лишь ref сохранённого invocation; чужой binding не служит fallback.
func assertPersonalInvocationCredentialPin(t *testing.T, ctx context.Context, repository *Repository, owner value.Principal, connection entity.IntegrationConnection, runRef string) {
	t.Helper()
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin isolated invocation credential fixture", err)
	}
	defer tx.Rollback(ctx)
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT public_configuration FROM control_plane.integration_connections WHERE ref=$1", connection.Ref).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var configuration map[string]any
	if json.Unmarshal(raw, &configuration) != nil {
		t.Fatal("decode synthetic invocation configuration")
	}
	canonical, err := json.Marshal(configuration)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(canonical)
	configurationDigest := hex.EncodeToString(digest[:])
	// These metadata-only writes construct an already verified fixture; no health HTTP is implied.
	// Эти metadata-only записи создают проверенный fixture; health HTTP ими не подтверждается.
	if _, err := tx.Exec(ctx, `INSERT INTO control_plane.integration_personal_credential_revisions
(ref,organization_id,binding_id,revision,secret_ref,secret_uid,secret_resource_version,content_sha256)
SELECT 'icr_personalpinfixture',binding.organization_id,binding.id,
 (SELECT max(revision)+1 FROM control_plane.integration_personal_credential_revisions WHERE binding_id=binding.id),
 'kodex-system/kodex-integration-credentials#synthetic-personal-pin',gen_random_uuid(),'synthetic-pin-version',repeat('a',64)
FROM control_plane.integration_personal_credentials binding
JOIN control_plane.integration_connections connection ON connection.id=binding.connection_id
WHERE connection.ref=$1 AND binding.subject_id=$2::uuid`, connection.Ref, owner.ActorID); err != nil {
		t.Fatal("create isolated personal pin revision", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE control_plane.integration_personal_credentials binding
SET credential_revision_id=revision.id,state='CHECK_REQUIRED',version=binding.version+1,generation=binding.generation+1
FROM control_plane.integration_personal_credential_revisions revision
WHERE revision.binding_id=binding.id AND revision.ref='icr_personalpinfixture'`); err != nil {
		t.Fatal("rotate isolated personal pin binding", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE control_plane.integration_personal_credentials binding
SET state='CHECKING',version=version+1 WHERE credential_revision_id=(
 SELECT id FROM control_plane.integration_personal_credential_revisions WHERE ref='icr_personalpinfixture')`); err != nil {
		t.Fatal("start isolated pin verification", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE control_plane.integration_personal_credentials binding
SET state='READY',version=binding.version+1,verified_credential_revision_id=binding.credential_revision_id,
 verified_definition_digest=connection.definition_digest,verified_configuration_digest=$1,
 verified_capabilities=ARRAY['gitlab.project.metadata.read']::text[]
FROM control_plane.integration_connections connection
WHERE binding.connection_id=connection.id AND binding.subject_id=$2::uuid AND connection.ref=$3`, configurationDigest, owner.ActorID, connection.Ref); err != nil {
		t.Fatal("finish isolated pin verification", err)
	}
	if _, err := tx.Exec(ctx, "UPDATE control_plane.runs SET state='RUNNING' WHERE ref=$1", runRef); err != nil {
		t.Fatal("activate isolated metadata root", err)
	}
	insert := func(ref string) {
		t.Helper()
		if _, err := tx.Exec(ctx, `INSERT INTO control_plane.integration_invocations
(ref,organization_id,run_id,node_id,connection_id,grant_id,capability_key,operation,idempotency_key,
 intent_digest,input_digest,bounded_input,state,definition_version,definition_digest,risk,approval_policy,
 resource_kind,resource_scope,resource_scope_digest,effect_key,grant_version,approval_scope_paths)
SELECT $1,execution.organization_id,execution.id,node.id,connection.id,grant_row.id,grant_row.capability_key,
 grant_row.capability_key,$1,repeat('a',64),repeat('b',64),'{}'::jsonb,'READY',connection.definition_version,
 connection.definition_digest,grant_row.risk,grant_row.approval_policy,grant_row.resource_kind,
 grant_row.resource_scope,grant_row.resource_scope_digest,'eff_'||$1,grant_row.version,'{}'::text[]
FROM control_plane.runs execution
JOIN control_plane.run_nodes node ON node.run_id=execution.id AND node.agent_id IS NOT NULL
JOIN control_plane.integration_connections connection ON connection.organization_id=execution.organization_id AND connection.ref=$2
JOIN control_plane.integration_grants grant_row ON grant_row.connection_id=connection.id AND grant_row.enabled
WHERE execution.ref=$3 LIMIT 1`, ref, connection.Ref, runRef); err != nil {
			t.Fatal("insert isolated invocation metadata", err)
		}
	}
	insert("inv_personalpinfixture")
	if err := repository.pinPersonalInvocationCredentialTx(ctx, tx, owner.AuthorityTenant, "inv_personalpinfixture"); err != nil {
		t.Fatal("pin root owner's exact personal credential", err)
	}
	var subject, revision string
	var generation int64
	if err := tx.QueryRow(ctx, `SELECT pin.subject_id::text,revision.ref,pin.binding_generation
FROM control_plane.integration_personal_invocation_credentials pin
JOIN control_plane.integration_invocations invocation ON invocation.id=pin.invocation_id
JOIN control_plane.integration_personal_credential_revisions revision ON revision.id=pin.credential_revision_id
WHERE invocation.ref='inv_personalpinfixture'`).Scan(&subject, &revision, &generation); err != nil || subject != owner.ActorID || revision != "icr_personalpinfixture" || generation < 1 {
		t.Fatal("invocation selected a foreign or shared key", err)
	}
	if err := repository.pinPersonalInvocationCredentialTx(ctx, tx, owner.AuthorityTenant, "inv_personalpinfixture"); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("duplicate pin insert overwrote immutable lineage", err)
	}
	assertPersonalInvocationAdmissionReceipt(t, ctx, repository, tx, owner.AuthorityTenant, "inv_personalpinfixture")
	if _, err := tx.Exec(ctx, `UPDATE control_plane.integration_personal_credentials binding
SET state='REVOKED',version=version+1,generation=generation+1,verified_credential_revision_id=NULL,
 verified_definition_digest=NULL,verified_configuration_digest=NULL,verified_capabilities='{}',denied_capabilities='{}',observed_permission_profiles='{}',expires_at=NULL
WHERE binding.subject_id=$1::uuid AND binding.connection_id=(SELECT id FROM control_plane.integration_connections WHERE ref=$2)`, owner.ActorID, connection.Ref); err != nil {
		t.Fatal("revoke isolated pinned credential", err)
	}
	insert("inv_personalpinrevoked")
	err = repository.pinPersonalInvocationCredentialTx(ctx, tx, owner.AuthorityTenant, "inv_personalpinrevoked")
	var failure *platformservice.IntegrationCredentialPreflightError
	if !errors.As(err, &failure) || len(failure.Issues) != 1 || failure.Issues[0].Problem != entity.IntegrationCredentialRevoked {
		t.Fatal("revoked root credential was replaced by another user or shared key", err)
	}
	var rejectedID string
	if err := tx.QueryRow(ctx, "SELECT id::text FROM control_plane.integration_invocations WHERE ref='inv_personalpinrevoked'").Scan(&rejectedID); err != nil {
		t.Fatal(err)
	}
	rejectedFence := sha256.Sum256([]byte("fnc_unstarted_fixture"))
	rejectedDigest := hex.EncodeToString(rejectedFence[:])
	if tag, err := tx.Exec(ctx, queryWorkersClaimintegrationinvocationsClaimInvocationLease, rejectedID,
		"lea_unstarted_fixture", rejectedDigest, int64(1), "personal-admission-fixture", time.Now().Add(time.Minute), "integration-gateway", "READY"); err != nil || tag.RowsAffected() != 1 {
		t.Fatal("prepare isolated unstarted personal denial lease", err)
	}
	deniedArgs := pgx.StrictNamedArgs{"organization_id": owner.AuthorityTenant, "invocation_ref": "inv_personalpinrevoked",
		"generation": int64(1), "lease_ref": "lea_foreign_unstarted", "fence_digest": rejectedDigest, "workload_instance": "personal-admission-fixture"}
	if tag, err := tx.Exec(ctx, queryPersonalInvocationDenyUnstarted, deniedArgs); err != nil || tag.RowsAffected() != 0 {
		t.Fatal("foreign lease closed an unstarted personal invocation", err)
	}
	deniedArgs["lease_ref"] = "lea_unstarted_fixture"
	if tag, err := tx.Exec(ctx, queryPersonalInvocationDenyUnstarted, deniedArgs); err != nil || tag.RowsAffected() != 1 {
		t.Fatal("owned unstarted denial did not close its exact invocation", err)
	}
	var deniedState, deniedCode string
	if err := tx.QueryRow(ctx, "SELECT state,safe_error_code FROM control_plane.integration_invocations WHERE ref='inv_personalpinrevoked'").Scan(&deniedState, &deniedCode); err != nil || deniedState != "FAILED" || deniedCode != "INTEGRATION_CREDENTIAL_UNAVAILABLE" {
		t.Fatal("unstarted denial did not preserve a bounded terminal failure", err)
	}
	var count int
	var retainedGeneration int64
	if err := tx.QueryRow(ctx, "SELECT count(*),min(binding_generation) FROM control_plane.integration_personal_invocation_credentials").Scan(&count, &retainedGeneration); err != nil || count != 1 || retainedGeneration != generation {
		t.Fatal("revoke changed historical pin or admitted a new intent", err)
	}
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, mutationErr := savepoint.Exec(ctx, "UPDATE control_plane.integration_personal_invocation_credentials SET binding_generation=binding_generation+1")
	if err := savepoint.Rollback(ctx); err != nil || mutationErr == nil {
		t.Fatal("runtime could mutate immutable invocation lineage", err)
	}
	// The fixture creates no external PVC/object. Qualify only the real database purge protocol,
	// including populated pins, while retaining organization-wide personal bindings and revisions.
	// Fixture не создаёт внешних PVC/objects. Проверяется только настоящий DB purge protocol,
	// включая заполненные pins и сохранение личных bindings/revisions уровня организации.
	resolved, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	current, err := repository.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	var projectID, projectRef string
	var projectVersion int64
	if err := tx.QueryRow(ctx, `SELECT project.id::text,project.ref,project.version
FROM control_plane.projects project JOIN control_plane.runs execution ON execution.project_id=project.id
WHERE execution.ref=$1`, runRef).Scan(&projectID, &projectRef, &projectVersion); err != nil {
		t.Fatal(err)
	}
	trashed, err := repository.changeProjectLifecycle(ctx, tx, current, command.Command{Kind: command.TrashProject,
		Mutation: value.Mutation{ExpectedVersion: &projectVersion}, Payload: command.ProjectLifecycleInput{Ref: projectRef}})
	if err != nil || trashed.result.Project == nil {
		t.Fatal("trash isolated project containing personal pin", err)
	}
	if _, err := repository.changeProjectLifecycle(ctx, tx, current, command.Command{Kind: command.PurgeProject,
		Mutation: value.Mutation{ExpectedVersion: &trashed.result.Project.Version}, Payload: command.ProjectLifecycleInput{Ref: projectRef}}); err != nil {
		t.Fatal("request isolated personal pin project purge", err)
	}
	var inventoryDigest string
	if err := tx.QueryRow(ctx, queryProjectPurgeDigest, owner.AuthorityTenant, projectID).Scan(&inventoryDigest); err != nil {
		t.Fatal("read isolated purge inventory digest", err)
	}
	if _, err := tx.Exec(ctx, queryProjectPurgeMarkObjectsCleared, owner.AuthorityTenant, projectID, inventoryDigest); err != nil {
		t.Fatal("ack absent external fixture resources", err)
	}
	var deleted int
	if err := tx.QueryRow(ctx, queryProjectPurgeDeleteDatabase, owner.AuthorityTenant, projectID, inventoryDigest).Scan(&deleted); err != nil || deleted < 1 {
		t.Fatal("purge populated personal credential pin through canonical owner function", err)
	}
	var pins, admissions, retainedBindings, retainedRevisions int
	if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM control_plane.integration_personal_invocation_credentials),
 (SELECT count(*) FROM control_plane.integration_personal_invocation_admissions),
 (SELECT count(*) FROM control_plane.integration_personal_credentials WHERE subject_id=$1::uuid),
 (SELECT count(*) FROM control_plane.integration_personal_credential_revisions WHERE ref='icr_personalpinfixture')`, owner.ActorID).
		Scan(&pins, &admissions, &retainedBindings, &retainedRevisions); err != nil || pins != 0 || admissions != 0 || retainedBindings < 1 || retainedRevisions != 1 {
		t.Fatal("project purge retained pins or deleted organization-wide personal history", err)
	}
}

// assertPersonalInvocationAdmissionReceipt qualifies the real SQL lease/pin/receipt boundary.
// It creates lease metadata directly and does not claim the full canonical runtime/Begin RPC path.
// assertPersonalInvocationAdmissionReceipt проверяет настоящую SQL lease/pin/receipt границу.
// Lease metadata создаются напрямую; полный canonical runtime/Begin RPC path не подтверждается.
func assertPersonalInvocationAdmissionReceipt(t *testing.T, ctx context.Context, repository *Repository, tx pgx.Tx, organizationID, invocationRef string) {
	t.Helper()
	choice, err := repository.selectPersonalInvocationCredentialTx(ctx, tx, organizationID, invocationRef)
	if err != nil {
		t.Fatal("resolve exact personal selection for admission fixture", err)
	}
	fence := sha256.Sum256([]byte("fnc_personal_admission_fixture"))
	fenceDigest := hex.EncodeToString(fence[:])
	for generation := int64(1); generation <= 2; generation++ {
		lease := "lea_personal_admission_first"
		if generation == 2 {
			lease = "lea_personal_admission_second"
			if _, err := tx.Exec(ctx, "UPDATE control_plane.integration_invocations SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE ref=$1", invocationRef); err != nil {
				t.Fatal("expire admitted isolated invocation", err)
			}
			if _, err := tx.Exec(ctx, queryWorkersClaimintegrationinvocationsExpireStaleInvocationLeases, organizationID, "integration-gateway"); err != nil {
				t.Fatal("expire admitted personal lease through production query", err)
			}
			var expiredState string
			if err := tx.QueryRow(ctx, "SELECT state FROM control_plane.integration_invocations WHERE ref=$1", invocationRef).Scan(&expiredState); err != nil || expiredState != "UNKNOWN_OUTCOME" {
				t.Fatal("admitted personal READ was automatically replayable after lost ACK", err)
			}
			// A direct fixture reset probes the unique EXECUTE fence, not a supported replay transition.
			// Прямой fixture reset проверяет unique EXECUTE fence, не поддержанный replay transition.
			if _, err := tx.Exec(ctx, `UPDATE control_plane.integration_invocations SET state='READY',lease_ref=NULL,effect_fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL WHERE ref=$1`, invocationRef); err != nil {
				t.Fatal("expire isolated read lease", err)
			}
		}
		tag, err := tx.Exec(ctx, queryWorkersClaimintegrationinvocationsClaimInvocationLease, choice.context.invocationID,
			lease, fenceDigest, generation, "personal-admission-fixture", time.Now().Add(time.Minute), "integration-gateway", "READY")
		if err != nil || tag.RowsAffected() != 1 {
			t.Fatal("claim isolated invocation lease metadata", err)
		}
		var recovery bool
		args := pgx.StrictNamedArgs{"organization_id": organizationID, "invocation_ref": invocationRef,
			"lease_ref": lease, "generation": generation, "fence_digest": fenceDigest, "workload_instance": "personal-admission-fixture",
			"connection_id": choice.context.connectionID, "root_run_id": choice.context.rootID, "subject_id": choice.context.subjectID,
			"binding_id": choice.bindingID, "binding_generation": choice.selection.BindingGeneration,
			"credential_ref": choice.selection.Credential.Ref, "definition_digest": choice.context.definitionDigest, "configuration_digest": choice.configurationDigest,
			"resource_permission_profile": choice.selection.ResourcePermissionProfile}
		if err := tx.QueryRow(ctx, queryPersonalInvocationBeginLease, args).Scan(&recovery); err != nil || recovery {
			t.Fatal("exact invocation lease/pin did not admit execution metadata", err)
		}
		args["lease_ref"] = "lea_foreign_admission_fixture"
		if err := tx.QueryRow(ctx, queryPersonalInvocationBeginLease, args).Scan(&recovery); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal("foreign invocation lease matched personal admission", err)
		}
		admission := pgx.StrictNamedArgs{"organization_id": organizationID, "invocation_id": choice.context.invocationID,
			"generation": generation, "lease_ref": lease, "fence_digest": fenceDigest, "workload_instance": "personal-admission-fixture", "recovery_read_only": false}
		tag, err = tx.Exec(ctx, queryPersonalInvocationAdmit, admission)
		expected := int64(1)
		if generation == 2 {
			expected = 0
		}
		if err != nil || tag.RowsAffected() != expected {
			t.Fatal("personal execution receipt admitted a second execution epoch", err)
		}
		tag, err = tx.Exec(ctx, queryPersonalInvocationAdmit, admission)
		if err != nil || tag.RowsAffected() != 0 {
			t.Fatal("lost ACK receipt replay authorized another external action", err)
		}
		denial := pgx.StrictNamedArgs{"organization_id": organizationID, "invocation_ref": invocationRef, "generation": generation,
			"lease_ref": lease, "fence_digest": fenceDigest, "workload_instance": "personal-admission-fixture"}
		if tag, err := tx.Exec(ctx, queryPersonalInvocationDenyUnstarted, denial); err != nil || tag.RowsAffected() != 0 {
			t.Fatal("denial erased admitted or uncertain work", err)
		}
	}
}
