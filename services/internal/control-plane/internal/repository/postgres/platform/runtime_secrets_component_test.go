package platform

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

const (
	runtimeSecretHashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	runtimeSecretHashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	runtimeSecretHashC = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

func testRuntimeSecretCrashConsistency(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct runtime secret service: %v", err)
	}
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Runtime secret owner", CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create",
	}, "control-api-gateway")
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-secret-project-create"},
		Payload:  command.ProjectInput{Name: "Runtime secret project", Language: "en"}})
	if err != nil || created.Project == nil {
		t.Fatalf("create runtime secret project: project=%#v err=%v", created.Project, err)
	}

	createPrincipal := runtimeSecretOwnerPrincipal(owner, "secret.create")
	createInput := platformrepo.RuntimeSecretPrepareInput{
		Kind: "CREATE", ProjectRef: created.Project.Ref, Name: "component-secret", Description: "Crash consistency fixture",
		ValueType: "STRING", ExpectedContentSHA256: runtimeSecretHashA,
		Mutation: value.Mutation{IdempotencyKey: "runtime-secret-create-1"},
	}
	firstPrepare, err := service.PrepareRuntimeSecretOperation(ctx, createPrincipal, createInput)
	if err != nil || firstPrepare.OperationGrant == "" || firstPrepare.OperationRef == "" {
		t.Fatalf("prepare create: result=%#v err=%v", firstPrepare, err)
	}
	reissued, err := service.PrepareRuntimeSecretOperation(ctx, createPrincipal, createInput)
	if err != nil || reissued.OperationRef != firstPrepare.OperationRef || reissued.OperationGrant == firstPrepare.OperationGrant {
		t.Fatalf("reissue prepared create: first=%#v second=%#v err=%v", firstPrepare, reissued, err)
	}
	differentIntent := createInput
	differentIntent.ExpectedContentSHA256 = runtimeSecretHashB
	if _, err := service.PrepareRuntimeSecretOperation(ctx, createPrincipal, differentIntent); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("same idempotency key accepted a different intent: %v", err)
	}

	consumePrincipal := runtimeSecretSystemPrincipal(t, ctx, repository, "platform.runtime-secrets.operations.consume")
	if _, err := service.ConsumeRuntimeSecretOperation(ctx, consumePrincipal, platformrepo.RuntimeSecretConsumeInput{
		OperationGrant: firstPrepare.OperationGrant, ClaimantID: "secret-broker-a",
	}); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("superseded grant was accepted: %v", err)
	}
	firstClaim, err := service.ConsumeRuntimeSecretOperation(ctx, consumePrincipal, platformrepo.RuntimeSecretConsumeInput{
		OperationGrant: reissued.OperationGrant, ClaimantID: "secret-broker-a",
	})
	if err != nil || firstClaim.ClaimGeneration != 1 || len(firstClaim.RevisionDescriptors) != 1 || firstClaim.ExpectedContentSHA256 != runtimeSecretHashA {
		t.Fatalf("claim create: claim=%#v err=%v", firstClaim, err)
	}
	if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.runtime_secret_operations SET claim_lease_deadline = clock_timestamp() - interval '1 second' WHERE ref = $1`, firstPrepare.OperationRef); err != nil {
		t.Fatalf("expire create claim: %v", err)
	}
	if _, err := service.PrepareRuntimeSecretOperation(ctx, createPrincipal, createInput); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("expired claimed operation was reissued: %v", err)
	}
	competingCreate := createInput
	competingCreate.Mutation.IdempotencyKey = "runtime-secret-create-claimed-competitor"
	if _, err := service.PrepareRuntimeSecretOperation(ctx, createPrincipal, competingCreate); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("expired claimed operation did not block a different idempotency key: %v", err)
	}
	name, err := runtimesecret.VersionedKubernetesName(firstClaim.SecretRef, firstClaim.TargetRevision)
	if err != nil {
		t.Fatalf("build materialization name: %v", err)
	}
	materialization := entity.RuntimeSecretMaterialization{
		Namespace: "kodex-runtime", SecretName: name, SecretKey: "value",
		SecretUID: "10000000-0000-4000-8000-000000000101", SecretResourceVersion: "101", ContentSHA256: runtimeSecretHashA,
		DisplayHint: &entity.RuntimeSecretDisplayHint{Prefix: "sec", Suffix: "ret"},
	}
	completePrincipal := runtimeSecretSystemPrincipal(t, ctx, repository, "platform.runtime-secrets.operations.complete")
	recoverPrincipal := runtimeSecretSystemPrincipal(t, ctx, repository, "platform.runtime-secrets.operations.recover")
	recovered, err := service.RecoverRuntimeSecretMaterialization(ctx, recoverPrincipal, platformrepo.RuntimeSecretRecoveryInput{
		OperationRef: firstPrepare.OperationRef, Materialization: materialization,
	})
	if err != nil || recovered.Action != "KEEP" || recovered.OperationState != "COMPLETED" || recovered.Secret == nil || recovered.Secret.CurrentRevision != 1 {
		t.Fatalf("recover expired create: result=%#v err=%v", recovered, err)
	}
	lostResponseRetry, err := service.PrepareRuntimeSecretOperation(ctx, createPrincipal, createInput)
	if err != nil || lostResponseRetry.State != "COMPLETED" || lostResponseRetry.OperationGrant != "" || lostResponseRetry.TerminalSecret == nil || lostResponseRetry.TerminalSecret.CurrentRevision != 1 {
		t.Fatalf("read terminal create receipt: result=%#v err=%v", lostResponseRetry, err)
	}
	assertRuntimeSecretAudit(t, ctx, repository, firstPrepare.OperationRef, "SUCCEEDED", 1)

	rotatePrincipal := runtimeSecretOwnerPrincipal(owner, "secret.rotate")
	expectedVersion := recovered.Secret.Version
	failPrincipal := runtimeSecretSystemPrincipal(t, ctx, repository, "platform.runtime-secrets.operations.fail")
	testAbandonedPreparedRuntimeSecretMutation(t, ctx, repository, service, rotatePrincipal, consumePrincipal, failPrincipal, *recovered.Secret)

	badRotateInput := platformrepo.RuntimeSecretPrepareInput{
		Kind: "ROTATE", SecretRef: recovered.Secret.Ref, ValueType: recovered.Secret.ValueType, ExpectedContentSHA256: runtimeSecretHashB,
		Mutation: value.Mutation{IdempotencyKey: "runtime-secret-rotate-bad", ExpectedVersion: &expectedVersion},
	}
	badRotate, err := service.PrepareRuntimeSecretOperation(ctx, rotatePrincipal, badRotateInput)
	if err != nil {
		t.Fatalf("prepare hash mismatch rotate: %v", err)
	}
	badClaim, err := service.ConsumeRuntimeSecretOperation(ctx, consumePrincipal, platformrepo.RuntimeSecretConsumeInput{
		OperationGrant: badRotate.OperationGrant, ClaimantID: "secret-broker-hash",
	})
	if err != nil {
		t.Fatalf("claim hash mismatch rotate: %v", err)
	}
	badName, _ := runtimesecret.VersionedKubernetesName(badClaim.SecretRef, badClaim.TargetRevision)
	badMaterialization := &entity.RuntimeSecretMaterialization{
		Namespace: "kodex-runtime", SecretName: badName, SecretKey: "value",
		SecretUID: "10000000-0000-4000-8000-000000000102", SecretResourceVersion: "102", ContentSHA256: runtimeSecretHashC,
	}
	if _, err := service.CompleteRuntimeSecretOperation(ctx, completePrincipal, platformrepo.RuntimeSecretCompleteInput{
		OperationRef: badRotate.OperationRef, ClaimantID: "secret-broker-hash", ClaimGeneration: badClaim.ClaimGeneration,
		Materialization: badMaterialization,
	}); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatalf("mismatched content hash was accepted: %v", err)
	}
	work, next, err := service.ListRuntimeSecretRecoveryWork(ctx, recoverPrincipal, platformrepo.RuntimeSecretRecoveryPage{Size: 100})
	if err != nil || len(work) != 0 || next != "" {
		t.Fatalf("live claim was listed for recovery: work=%#v next=%q err=%v", work, next, err)
	}
	if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.runtime_secret_operations SET claim_lease_deadline = clock_timestamp() - interval '1 second' WHERE ref = $1`, badRotate.OperationRef); err != nil {
		t.Fatalf("expire invalid materialization claim: %v", err)
	}
	work, next, err = service.ListRuntimeSecretRecoveryWork(ctx, recoverPrincipal, platformrepo.RuntimeSecretRecoveryPage{Size: 100})
	if err != nil || len(work) != 1 || next != "" {
		t.Fatalf("expired claim recovery page: work=%#v next=%q err=%v", work, next, err)
	}
	listed := work[0]
	if listed.OperationRef != badRotate.OperationRef || listed.Kind != "ROTATE" || listed.ClaimantID != "secret-broker-hash" ||
		listed.ClaimGeneration != badClaim.ClaimGeneration || listed.Namespace != "kodex-runtime" || listed.SecretRef != badClaim.SecretRef ||
		listed.TargetRevision != badClaim.TargetRevision || listed.SecretKey != "value" || listed.ExpectedContentSHA256 != runtimeSecretHashB {
		t.Fatalf("expired recovery work leaked or omitted coordinates: %#v", listed)
	}
	if _, err := service.FailRuntimeSecretOperation(ctx, failPrincipal, platformrepo.RuntimeSecretFailInput{
		OperationRef: badRotate.OperationRef, ClaimantID: "secret-broker-hash", ClaimGeneration: badClaim.ClaimGeneration + 1,
		FailureCode: "MATERIALIZATION_INVALID",
	}); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("wrong recovery fence was accepted: %v", err)
	}
	if _, err := service.FailRuntimeSecretOperation(ctx, failPrincipal, platformrepo.RuntimeSecretFailInput{
		OperationRef: badRotate.OperationRef, ClaimantID: "secret-broker-hash", ClaimGeneration: badClaim.ClaimGeneration,
		FailureCode: "GRANT_EXPIRED",
	}); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("broker supplied internal GRANT_EXPIRED: %v", err)
	}
	failed, err := service.FailRuntimeSecretOperation(ctx, failPrincipal, platformrepo.RuntimeSecretFailInput{
		OperationRef: badRotate.OperationRef, ClaimantID: "secret-broker-hash", ClaimGeneration: badClaim.ClaimGeneration,
		FailureCode: "MATERIALIZATION_INVALID",
	})
	if err != nil || failed.State != "FAILED" {
		t.Fatalf("fail invalid materialization: result=%#v err=%v", failed, err)
	}
	if _, err := service.FailRuntimeSecretOperation(ctx, failPrincipal, platformrepo.RuntimeSecretFailInput{
		OperationRef: badRotate.OperationRef, ClaimantID: "secret-broker-hash", ClaimGeneration: badClaim.ClaimGeneration,
		FailureCode: "MATERIALIZATION_INVALID",
	}); err != nil {
		t.Fatalf("retry terminal failure: %v", err)
	}
	assertRuntimeSecretAudit(t, ctx, repository, badRotate.OperationRef, "FAILED", 1)
	work, next, err = service.ListRuntimeSecretRecoveryWork(ctx, recoverPrincipal, platformrepo.RuntimeSecretRecoveryPage{Size: 100})
	if err != nil || len(work) != 0 || next != "" {
		t.Fatalf("terminal operation remained in recovery work: work=%#v next=%q err=%v", work, next, err)
	}
	failedRetry, err := service.PrepareRuntimeSecretOperation(ctx, rotatePrincipal, badRotateInput)
	if err != nil || failedRetry.State != "FAILED" || failedRetry.FailureCode != "MATERIALIZATION_INVALID" || failedRetry.OperationGrant != "" {
		t.Fatalf("read terminal failure receipt: result=%#v err=%v", failedRetry, err)
	}

	current := *recovered.Secret
	current.Version = recovered.Secret.Version
	completedRotate := completeRuntimeSecretRotate(t, ctx, service, rotatePrincipal, consumePrincipal, completePrincipal, current, runtimeSecretHashB, "runtime-secret-rotate-good")
	obsolete, err := service.RecoverRuntimeSecretMaterialization(ctx, recoverPrincipal, platformrepo.RuntimeSecretRecoveryInput{
		OperationRef: firstPrepare.OperationRef, Materialization: materialization,
	})
	if err != nil || obsolete.Action != "DELETE" {
		t.Fatalf("obsolete revision was not deleted: result=%#v err=%v", obsolete, err)
	}

	revealPrincipal := runtimeSecretOwnerPrincipal(owner, "secret.reveal")
	revealPrincipal.CredentialAuthenticatedAt = time.Now().UTC()
	revealInput := platformrepo.RuntimeSecretPrepareInput{
		Kind: "REVEAL", SecretRef: completedRotate.Ref,
		Mutation: value.Mutation{IdempotencyKey: "runtime-secret-reveal-once", ExpectedVersion: &completedRotate.Version},
	}
	reveal, err := service.PrepareRuntimeSecretOperation(ctx, revealPrincipal, revealInput)
	if err != nil {
		t.Fatalf("prepare reveal: %v", err)
	}
	revealClaim, err := service.ConsumeRuntimeSecretOperation(ctx, consumePrincipal, platformrepo.RuntimeSecretConsumeInput{
		OperationGrant: reveal.OperationGrant, ClaimantID: "secret-broker-reveal",
	})
	if err != nil || len(revealClaim.RevisionDescriptors) != 1 || revealClaim.RevisionDescriptors[0].Revision != completedRotate.CurrentRevision {
		t.Fatalf("claim reveal: claim=%#v err=%v", revealClaim, err)
	}
	if _, err := service.CompleteRuntimeSecretOperation(ctx, completePrincipal, platformrepo.RuntimeSecretCompleteInput{
		OperationRef: reveal.OperationRef, ClaimantID: "secret-broker-reveal", ClaimGeneration: revealClaim.ClaimGeneration,
	}); err != nil {
		t.Fatalf("complete reveal: %v", err)
	}
	if _, err := service.PrepareRuntimeSecretOperation(ctx, revealPrincipal, revealInput); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("successful reveal was reissued: %v", err)
	}

	revokePrincipal := runtimeSecretOwnerPrincipal(owner, "secret.revoke")
	revokeInput := platformrepo.RuntimeSecretPrepareInput{
		Kind: "REVOKE", SecretRef: completedRotate.Ref,
		Mutation: value.Mutation{IdempotencyKey: "runtime-secret-revoke-terminal", ExpectedVersion: &completedRotate.Version},
	}
	revoke, err := service.PrepareRuntimeSecretOperation(ctx, revokePrincipal, revokeInput)
	if err != nil {
		t.Fatalf("prepare revoke: %v", err)
	}
	revokeClaim, err := service.ConsumeRuntimeSecretOperation(ctx, consumePrincipal, platformrepo.RuntimeSecretConsumeInput{
		OperationGrant: revoke.OperationGrant, ClaimantID: "secret-broker-revoke",
	})
	if err != nil || len(revokeClaim.RevisionDescriptors) != 2 {
		t.Fatalf("claim revoke: claim=%#v err=%v", revokeClaim, err)
	}
	revoked, err := service.CompleteRuntimeSecretOperation(ctx, completePrincipal, platformrepo.RuntimeSecretCompleteInput{
		OperationRef: revoke.OperationRef, ClaimantID: "secret-broker-revoke", ClaimGeneration: revokeClaim.ClaimGeneration,
	})
	if err != nil || revoked.State != "REVOKED" || revoked.Version <= completedRotate.Version {
		t.Fatalf("complete revoke: secret=%#v err=%v", revoked, err)
	}
	revokeRetry, err := service.PrepareRuntimeSecretOperation(ctx, revokePrincipal, revokeInput)
	if err != nil || revokeRetry.State != "COMPLETED" || revokeRetry.TerminalSecret == nil || revokeRetry.TerminalSecret.State != "REVOKED" {
		t.Fatalf("read terminal revoke receipt: result=%#v err=%v", revokeRetry, err)
	}
	latestMaterialization := materialization
	latestMaterialization.SecretName = completedRotate.CurrentRevisionDescriptor.SecretName
	latestMaterialization.SecretUID = completedRotate.CurrentRevisionDescriptor.SecretUID
	latestMaterialization.SecretResourceVersion = completedRotate.CurrentRevisionDescriptor.SecretResourceVersion
	latestMaterialization.ContentSHA256 = completedRotate.CurrentRevisionDescriptor.ContentSHA256
	afterRevoke, err := service.RecoverRuntimeSecretMaterialization(ctx, recoverPrincipal, platformrepo.RuntimeSecretRecoveryInput{
		OperationRef: revoke.OperationRef, Materialization: latestMaterialization,
	})
	if err != nil || afterRevoke.Action != "DELETE" {
		t.Fatalf("revoked materialization was not deleted: result=%#v err=%v", afterRevoke, err)
	}
	assertRuntimeSecretAudit(t, ctx, repository, revoke.OperationRef, "SUCCEEDED", 1)

	testConcurrentRuntimeSecretRotateRevoke(t, ctx, repository, service, owner, consumePrincipal, failPrincipal)
}

func testAbandonedPreparedRuntimeSecretMutation(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, rotate, consume, fail value.Principal, secret entity.RuntimeSecret) {
	t.Helper()
	expectedVersion := secret.Version
	abandonedInput := platformrepo.RuntimeSecretPrepareInput{
		Kind: "ROTATE", SecretRef: secret.Ref, ValueType: secret.ValueType, ExpectedContentSHA256: runtimeSecretHashB,
		Mutation: value.Mutation{IdempotencyKey: "runtime-secret-abandoned-prepared", ExpectedVersion: &expectedVersion},
	}
	abandoned, err := service.PrepareRuntimeSecretOperation(ctx, rotate, abandonedInput)
	if err != nil {
		t.Fatalf("prepare abandoned mutation: %v", err)
	}
	if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.runtime_secret_operations SET grant_expires_at = clock_timestamp() - interval '1 second' WHERE ref = $1`, abandoned.OperationRef); err != nil {
		t.Fatalf("expire prepared mutation: %v", err)
	}
	reissued, err := service.PrepareRuntimeSecretOperation(ctx, rotate, abandonedInput)
	if err != nil || reissued.OperationRef != abandoned.OperationRef || reissued.OperationGrant == "" || reissued.OperationGrant == abandoned.OperationGrant {
		t.Fatalf("reissue expired prepared mutation: result=%#v err=%v", reissued, err)
	}
	if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.runtime_secret_operations SET grant_expires_at = clock_timestamp() - interval '1 second' WHERE ref = $1`, abandoned.OperationRef); err != nil {
		t.Fatalf("expire reissued prepared mutation: %v", err)
	}

	replacementInput := abandonedInput
	replacementInput.Mutation.IdempotencyKey = "runtime-secret-after-abandoned-prepared"
	replacement, err := service.PrepareRuntimeSecretOperation(ctx, rotate, replacementInput)
	if err != nil || replacement.State != "PREPARED" || replacement.OperationRef == abandoned.OperationRef {
		t.Fatalf("prepare replacement mutation: result=%#v err=%v", replacement, err)
	}

	abandonedReceipt, err := service.PrepareRuntimeSecretOperation(ctx, rotate, abandonedInput)
	if err != nil || abandonedReceipt.State != "FAILED" || abandonedReceipt.FailureCode != "GRANT_EXPIRED" || abandonedReceipt.OperationGrant != "" {
		t.Fatalf("read abandoned terminal receipt: result=%#v err=%v", abandonedReceipt, err)
	}
	var state, failureCode string
	var claimantID *string
	var generation int64
	var terminalAt *time.Time
	if err := repository.pool.QueryRow(ctx, `
		SELECT state, terminal_error_code, claimant_id, claim_generation, terminal_at
		FROM control_plane.runtime_secret_operations
		WHERE ref = $1`, abandoned.OperationRef).Scan(&state, &failureCode, &claimantID, &generation, &terminalAt); err != nil {
		t.Fatalf("read abandoned operation: %v", err)
	}
	if state != "FAILED" || failureCode != "GRANT_EXPIRED" || claimantID != nil || generation != 0 || terminalAt == nil {
		t.Fatalf("unexpected abandoned terminal state: state=%s code=%s claimant=%v generation=%d terminal=%v", state, failureCode, claimantID, generation, terminalAt)
	}
	assertRuntimeSecretAudit(t, ctx, repository, abandoned.OperationRef, "FAILED", 1)

	blockedInput := abandonedInput
	blockedInput.Mutation.IdempotencyKey = "runtime-secret-live-prepared-competitor"
	if _, err := service.PrepareRuntimeSecretOperation(ctx, rotate, blockedInput); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("non-expired prepared operation did not block competitor: %v", err)
	}

	claim, err := service.ConsumeRuntimeSecretOperation(ctx, consume, platformrepo.RuntimeSecretConsumeInput{
		OperationGrant: replacement.OperationGrant, ClaimantID: "secret-broker-abandoned-replacement",
	})
	if err != nil {
		t.Fatalf("claim replacement mutation: %v", err)
	}
	if _, err := service.FailRuntimeSecretOperation(ctx, fail, platformrepo.RuntimeSecretFailInput{
		OperationRef: replacement.OperationRef, ClaimantID: "secret-broker-abandoned-replacement",
		ClaimGeneration: claim.ClaimGeneration, FailureCode: "RECONCILIATION_FAILED",
	}); err != nil {
		t.Fatalf("finish replacement mutation: %v", err)
	}
}

func completeRuntimeSecretRotate(t *testing.T, ctx context.Context, service *platformservice.Service, rotate, consume, complete value.Principal, secret entity.RuntimeSecret, hash, key string) entity.RuntimeSecret {
	t.Helper()
	expectedVersion := secret.Version
	prepared, err := service.PrepareRuntimeSecretOperation(ctx, rotate, platformrepo.RuntimeSecretPrepareInput{
		Kind: "ROTATE", SecretRef: secret.Ref, ValueType: secret.ValueType, ExpectedContentSHA256: hash,
		Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &expectedVersion},
	})
	if err != nil {
		t.Fatalf("prepare rotate: %v", err)
	}
	claimed, err := service.ConsumeRuntimeSecretOperation(ctx, consume, platformrepo.RuntimeSecretConsumeInput{
		OperationGrant: prepared.OperationGrant, ClaimantID: "secret-broker-rotate",
	})
	if err != nil {
		t.Fatalf("claim rotate: %v", err)
	}
	name, _ := runtimesecret.VersionedKubernetesName(secret.Ref, claimed.TargetRevision)
	result, err := service.CompleteRuntimeSecretOperation(ctx, complete, platformrepo.RuntimeSecretCompleteInput{
		OperationRef: prepared.OperationRef, ClaimantID: "secret-broker-rotate", ClaimGeneration: claimed.ClaimGeneration,
		Materialization: &entity.RuntimeSecretMaterialization{
			Namespace: "kodex-runtime", SecretName: name, SecretKey: "value",
			SecretUID: "10000000-0000-4000-8000-000000000103", SecretResourceVersion: "103", ContentSHA256: hash,
		},
	})
	if err != nil {
		t.Fatalf("complete rotate: %v", err)
	}
	return result
}

func testConcurrentRuntimeSecretRotateRevoke(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner, consume, fail value.Principal) {
	t.Helper()
	create := runtimeSecretOwnerPrincipal(owner, "secret.create")
	prepared, err := service.PrepareRuntimeSecretOperation(ctx, create, platformrepo.RuntimeSecretPrepareInput{
		Kind: "CREATE", ProjectRef: runtimeSecretProjectRef(t, ctx, repository), Name: "concurrent-secret", ValueType: "STRING",
		ExpectedContentSHA256: runtimeSecretHashA, Mutation: value.Mutation{IdempotencyKey: "runtime-secret-concurrent-create"},
	})
	if err != nil {
		t.Fatalf("prepare concurrent fixture: %v", err)
	}
	claim, err := service.ConsumeRuntimeSecretOperation(ctx, consume, platformrepo.RuntimeSecretConsumeInput{OperationGrant: prepared.OperationGrant, ClaimantID: "secret-broker-concurrent-create"})
	if err != nil {
		t.Fatalf("claim concurrent fixture: %v", err)
	}
	name, _ := runtimesecret.VersionedKubernetesName(claim.SecretRef, claim.TargetRevision)
	complete := runtimeSecretSystemPrincipal(t, ctx, repository, "platform.runtime-secrets.operations.complete")
	secret, err := service.CompleteRuntimeSecretOperation(ctx, complete, platformrepo.RuntimeSecretCompleteInput{
		OperationRef: prepared.OperationRef, ClaimantID: "secret-broker-concurrent-create", ClaimGeneration: claim.ClaimGeneration,
		Materialization: &entity.RuntimeSecretMaterialization{Namespace: "kodex-runtime", SecretName: name, SecretKey: "value",
			SecretUID: "10000000-0000-4000-8000-000000000104", SecretResourceVersion: "104", ContentSHA256: runtimeSecretHashA},
	})
	if err != nil {
		t.Fatalf("complete concurrent fixture: %v", err)
	}

	type attempt struct {
		kind      string
		principal value.Principal
		input     platformrepo.RuntimeSecretPrepareInput
		result    platformrepo.RuntimeSecretPrepareResult
		err       error
	}
	expectedVersion := secret.Version
	attempts := []*attempt{
		{kind: "ROTATE", principal: runtimeSecretOwnerPrincipal(owner, "secret.rotate"), input: platformrepo.RuntimeSecretPrepareInput{
			Kind: "ROTATE", SecretRef: secret.Ref, ValueType: secret.ValueType, ExpectedContentSHA256: runtimeSecretHashB,
			Mutation: value.Mutation{IdempotencyKey: "runtime-secret-concurrent-rotate", ExpectedVersion: &expectedVersion},
		}},
		{kind: "REVOKE", principal: runtimeSecretOwnerPrincipal(owner, "secret.revoke"), input: platformrepo.RuntimeSecretPrepareInput{
			Kind: "REVOKE", SecretRef: secret.Ref,
			Mutation: value.Mutation{IdempotencyKey: "runtime-secret-concurrent-revoke", ExpectedVersion: &expectedVersion},
		}},
	}
	start := make(chan struct{})
	var wait sync.WaitGroup
	for _, candidate := range attempts {
		wait.Add(1)
		go func(item *attempt) {
			defer wait.Done()
			<-start
			item.result, item.err = service.PrepareRuntimeSecretOperation(ctx, item.principal, item.input)
		}(candidate)
	}
	close(start)
	wait.Wait()
	var winner *attempt
	blocked := 0
	for _, candidate := range attempts {
		if candidate.err == nil {
			if winner != nil {
				t.Fatalf("two concurrent mutations were prepared: %#v", attempts)
			}
			winner = candidate
		} else if errors.Is(candidate.err, domainerrs.ErrConflict) || errors.Is(candidate.err, domainerrs.ErrUnavailable) {
			// SERIALIZABLE может оборвать проигравшую транзакцию до чтения
			// победившего intent. Оба результата допускают безопасный повтор.
			blocked++
		} else {
			t.Fatalf("unexpected concurrent mutation error: kind=%s err=%v", candidate.kind, candidate.err)
		}
	}
	if winner == nil || blocked != 1 {
		t.Fatalf("concurrent mutation fence mismatch: attempts=%#v", attempts)
	}
	winnerClaim, err := service.ConsumeRuntimeSecretOperation(ctx, consume, platformrepo.RuntimeSecretConsumeInput{
		OperationGrant: winner.result.OperationGrant, ClaimantID: "secret-broker-concurrent-winner",
	})
	if err != nil {
		t.Fatalf("claim concurrent winner: %v", err)
	}
	if _, err := service.FailRuntimeSecretOperation(ctx, fail, platformrepo.RuntimeSecretFailInput{
		OperationRef: winner.result.OperationRef, ClaimantID: "secret-broker-concurrent-winner",
		ClaimGeneration: winnerClaim.ClaimGeneration, FailureCode: "RECONCILIATION_FAILED",
	}); err != nil {
		t.Fatalf("fail concurrent winner: %v", err)
	}
	assertRuntimeSecretAudit(t, ctx, repository, winner.result.OperationRef, "FAILED", 1)
}

func runtimeSecretOwnerPrincipal(base value.Principal, permission string) value.Principal {
	result := base
	result.Permission = permission
	result.CorrelationRef = permission + "-component"
	return result
}

func runtimeSecretSystemPrincipal(t *testing.T, ctx context.Context, repository *Repository, permission string) value.Principal {
	t.Helper()
	return resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "secret-broker", Operation: permission,
	}, "secret-broker")
}

func runtimeSecretProjectRef(t *testing.T, ctx context.Context, repository *Repository) string {
	t.Helper()
	var ref string
	if err := repository.pool.QueryRow(ctx, `SELECT ref FROM control_plane.projects WHERE name = 'Runtime secret project'`).Scan(&ref); err != nil {
		t.Fatalf("read runtime secret project: %v", err)
	}
	return ref
}

func assertRuntimeSecretAudit(t *testing.T, ctx context.Context, repository *Repository, operationRef, outcome string, expected int) {
	t.Helper()
	var count int
	if err := repository.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM control_plane.runtime_secret_operation_audits receipt
		JOIN control_plane.runtime_secret_operations operation ON operation.id = receipt.operation_id
		JOIN control_plane.audit_events audit ON audit.id = receipt.audit_event_id
		WHERE operation.ref = $1 AND audit.outcome = $2`, operationRef, outcome).Scan(&count); err != nil {
		t.Fatalf("read runtime secret audit: %v", err)
	}
	if count != expected {
		t.Fatalf("runtime secret audit count=%d want=%d operation=%s outcome=%s", count, expected, operationRef, outcome)
	}
}

func TestRuntimeSecretDigestComparison(t *testing.T) {
	if !runtimeSecretDigestsEqual(runtimeSecretHashA, strings.Repeat("a", 64)) {
		t.Fatal("equal SHA-256 digests did not match")
	}
	if runtimeSecretDigestsEqual(runtimeSecretHashA, runtimeSecretHashB) || runtimeSecretDigestsEqual(runtimeSecretHashA, "invalid") {
		t.Fatal("different or malformed SHA-256 digest matched")
	}
}
