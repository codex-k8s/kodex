package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testConfigurationWriteBackLifecycle(t *testing.T, ctx context.Context, repository *Repository, service *serviceplatform.Service, owner, worker value.Principal, set entity.ManagedConfigurationSet, base string) {
	t.Helper()
	proposed := strings.Replace(base, "Synthetic HTTP", "Synthetic writeback", 1)
	if proposed == base {
		t.Fatal("writeback fixture did not modify document")
	}
	version := set.Version
	prepare := command.Command{Kind: command.PrepareIntegrationDefinitionGitWriteBack, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "writeback-prepare", ExpectedVersion: &version}, Payload: command.ConfigurationWriteBackInput{ConfigurationRef: set.Ref, ExpectedSourceVersion: set.GitSource.Version, Content: proposed}}
	prepared, err := service.Execute(ctx, prepare)
	if err != nil || prepared.ConfigurationWriteBack == nil {
		t.Fatalf("writeback prepare: %v", err)
	}
	p := *prepared.ConfigurationWriteBack
	if p.State != entity.WriteBackWaiting || p.BaseContentSHA256 != writeBackDigest([]byte(base)) || p.ApprovalDigest == "" || p.ProposalBranch == set.GitSource.RefName {
		t.Fatal("writeback plan lost pins")
	}
	replay, err := service.Execute(ctx, prepare)
	if err != nil || replay.ConfigurationWriteBack.Ref != p.Ref {
		t.Fatalf("writeback prepare replay: %v", err)
	}
	view, err := service.GetConfigurationWriteBack(ctx, owner, p.Ref)
	if err != nil || view.BaseContent != base || view.ProposedContent != proposed || len(view.Proposal.NextActions) != 3 {
		t.Fatalf("writeback exact plan read: %v", err)
	}
	items, next, total, err := service.ListConfigurationWriteBacks(ctx, owner, set.Ref, query.Filter{Page: query.Page{Size: 1}})
	if err != nil || len(items) != 1 || total != 1 || next != "" {
		t.Fatalf("writeback list: %v", err)
	}
	worker.Permission = "platform.configuration-writebacks.work.claim"
	if work, err := service.ClaimConfigurationWriteBackWork(ctx, worker, "writeback-fixture", 1); err != nil || len(work) != 0 {
		t.Fatalf("unapproved proposal claimed: %v", err)
	}
	approveVersion := p.Version
	approve := command.Command{Kind: command.ApproveManagedConfigurationGitWriteBack, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "writeback-approve", ExpectedVersion: &approveVersion}, Payload: command.ConfigurationWriteBackInput{ProposalRef: p.Ref, ApprovalDigest: p.ApprovalDigest}}
	approved, err := service.Execute(ctx, approve)
	if err != nil || approved.ConfigurationWriteBack.State != entity.WriteBackQueued {
		t.Fatalf("writeback approve: %v", err)
	}
	work, err := service.ClaimConfigurationWriteBackWork(ctx, worker, "writeback-fixture", 1)
	if err != nil || len(work) != 1 || work[0].Mode != "EXECUTE" || work[0].Effect != entity.WriteBackBranch {
		t.Fatalf("writeback branch claim: %v", err)
	}
	begin := port.ConfigurationWriteBackEffectInput{Lease: work[0].Lease, Effect: entity.WriteBackBranch, CandidateCommitSHA: strings.Repeat("b", 40), CandidateTreeSHA: strings.Repeat("c", 40), CandidateBlobSHA: strings.Repeat("d", 40), ParentCommitSHA: p.BaseCommitSHA, ContentSHA256: p.ProposedContentSHA256, BaseBlobSHA: strings.Repeat("e", 40)}
	worker.Permission = "platform.configuration-writebacks.work.begin"
	started, duplicate, err := service.BeginConfigurationWriteBackEffect(ctx, worker, begin)
	if err != nil || duplicate || started.State != entity.WriteBackStarted {
		t.Fatalf("writeback branch begin: %v", err)
	}
	if _, duplicate, err = service.BeginConfigurationWriteBackEffect(ctx, worker, begin); err != nil || !duplicate {
		t.Fatalf("repeated begin reauthorized effect: %v", err)
	}
	tampered := begin
	tampered.CandidateCommitSHA = strings.Repeat("f", 40)
	if _, _, err := service.BeginConfigurationWriteBackEffect(ctx, worker, tampered); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("writeback begin digest mismatch: %v", err)
	}
	worker.Permission = "platform.configuration-writebacks.work.fail"
	unknown, err := service.FailConfigurationWriteBackWork(ctx, worker, begin.Lease, "UNAVAILABLE")
	if err != nil || unknown.State != entity.WriteBackUnknown {
		t.Fatalf("writeback uncertain branch: %v", err)
	}
	if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.managed_configuration_writebacks SET lease_expires_at=clock_timestamp()-interval '1 second',version=version+1 WHERE ref=$1`, p.Ref); err != nil {
		t.Fatal(err)
	}
	worker.Permission = "platform.configuration-writebacks.work.claim"
	work, err = service.ClaimConfigurationWriteBackWork(ctx, worker, "writeback-recovery", 1)
	if err != nil || len(work) != 1 || work[0].Mode != "RECOVER_READ_ONLY" || work[0].Proposal.CandidateCommitSHA != begin.CandidateCommitSHA {
		t.Fatalf("writeback read-only recovery: %v", err)
	}
	worker.Permission = "platform.configuration-writebacks.work.begin"
	tampered = begin
	tampered.Lease = work[0].Lease
	if _, _, err := service.BeginConfigurationWriteBackEffect(ctx, worker, tampered); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("recovery allowed resend: %v", err)
	}
	complete := port.ConfigurationWriteBackEffectInput{Lease: work[0].Lease, Effect: entity.WriteBackBranch, CandidateCommitSHA: begin.CandidateCommitSHA, ContentSHA256: p.ProposedContentSHA256}
	worker.Permission = "platform.configuration-writebacks.work.complete"
	branch, err := service.CompleteConfigurationWriteBackEffect(ctx, worker, complete)
	if err != nil || branch.State != entity.WriteBackQueued || branch.BranchConfirmedAt == nil || branch.PullRequestRef != "" {
		t.Fatalf("writeback branch receipt: %v", err)
	}
	worker.Permission = "platform.configuration-writebacks.work.claim"
	work, err = service.ClaimConfigurationWriteBackWork(ctx, worker, "writeback-pr", 1)
	if err != nil || len(work) != 1 || work[0].Effect != entity.WriteBackPullRequest || work[0].Mode != "EXECUTE" {
		t.Fatalf("writeback PR claim: %v", err)
	}
	begin.Lease, begin.Effect = work[0].Lease, entity.WriteBackPullRequest
	worker.Permission = "platform.configuration-writebacks.work.begin"
	if _, _, err := service.BeginConfigurationWriteBackEffect(ctx, worker, begin); err != nil {
		t.Fatalf("writeback PR begin: %v", err)
	}
	complete = port.ConfigurationWriteBackEffectInput{Lease: work[0].Lease, Effect: entity.WriteBackPullRequest, CandidateCommitSHA: begin.CandidateCommitSHA, ContentSHA256: p.ProposedContentSHA256, PullRequestRef: "17", PullRequestURL: "https://github.com/example/configuration/pull/17"}
	worker.Permission = "platform.configuration-writebacks.work.complete"
	finished, err := service.CompleteConfigurationWriteBackEffect(ctx, worker, complete)
	if err != nil || finished.State != entity.WriteBackSucceeded || finished.PullRequestConfirmedAt == nil {
		t.Fatalf("writeback PR receipt: %v", err)
	}
	if replay, err := service.CompleteConfigurationWriteBackEffect(ctx, worker, complete); err != nil || replay.Version != finished.Version {
		t.Fatalf("writeback completion replay: %v", err)
	}
	read, _, _, _, err := service.ListManagedConfigurationHistory(ctx, owner, set.Ref, query.Page{Size: 10})
	if err != nil || read.Version != set.Version || read.SourceRevision != set.SourceRevision || read.GitSource.AcceptedRevisionRef != set.GitSource.AcceptedRevisionRef {
		t.Fatalf("writeback published runtime before merge: %v", err)
	}
	view, err = service.GetConfigurationWriteBack(ctx, owner, p.Ref)
	if err != nil || view.Proposal.State != entity.WriteBackSucceeded {
		t.Fatalf("writeback terminal tombstone: %v", err)
	}
	for _, decision := range []command.Kind{command.RejectManagedConfigurationGitWriteBack, command.CancelManagedConfigurationGitWriteBack} {
		prepare.Mutation.IdempotencyKey = "writeback-prepare-" + string(decision)
		created, err := service.Execute(ctx, prepare)
		if err != nil {
			t.Fatalf("writeback terminal fixture prepare: %v", err)
		}
		item := created.ConfigurationWriteBack
		stale := item.Version + 1
		decisionCommand := command.Command{Kind: decision, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "writeback-decision-" + string(decision), ExpectedVersion: &stale}, Payload: command.ConfigurationWriteBackInput{ProposalRef: item.Ref, ApprovalDigest: item.ApprovalDigest}}
		if _, err := service.Execute(ctx, decisionCommand); !errors.Is(err, errs.ErrVersionMismatch) {
			t.Fatalf("writeback decision stale OCC: %v", err)
		}
		decisionCommand.Mutation.ExpectedVersion = &item.Version
		decided, err := service.Execute(ctx, decisionCommand)
		if err != nil || decided.ConfigurationWriteBack.CompletedAt == nil {
			t.Fatalf("writeback terminal decision: %v", err)
		}
		if _, err := service.Execute(ctx, decisionCommand); err != nil {
			t.Fatalf("writeback decision replay: %v", err)
		}
		worker.Permission = "platform.configuration-writebacks.work.claim"
		if work, err := service.ClaimConfigurationWriteBackWork(ctx, worker, "writeback-terminal", 16); err != nil || len(work) != 0 {
			t.Fatalf("terminal proposal reclaimed: %v", err)
		}
	}
	// Истёкший proposal моделируется только INSERT fixture; immutable expiry не переписывается.
	resolved, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	current, err := repository.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := lockWriteBack(ctx, tx, current, p.Ref)
	_ = tx.Rollback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	expired := stored.snapshot
	expired.Proposal.Ref = "mcwb_expired_fixture"
	expired.Proposal.ProposalBranch = "kodex/writeback/" + expired.Proposal.Ref
	expired.Proposal.ApprovalDigest = ""
	expired.Proposal.ApprovalDigest = writeBackDigest(asJSON(expired))
	raw := asJSON(expired)
	_, err = repository.pool.Exec(ctx, `INSERT INTO control_plane.managed_configuration_writebacks
 (ref,organization_id,configuration_set_id,source_id,root_actor_id,connection_id,credential_revision_id,input_snapshot,input_sha256,approval_digest,state,created_at,expires_at)
 SELECT $2,organization_id,configuration_set_id,source_id,root_actor_id,connection_id,credential_revision_id,$3::jsonb,$4,$5,'WAITING_APPROVAL',clock_timestamp()-interval '25 hours',clock_timestamp()-interval '1 hour'
 FROM control_plane.managed_configuration_writebacks WHERE ref=$1`, p.Ref, expired.Proposal.Ref, raw, writeBackDigest(raw), expired.Proposal.ApprovalDigest)
	if err != nil {
		t.Fatalf("expired writeback fixture: %v", err)
	}
	worker.Permission = "platform.configuration-writebacks.work.claim"
	if work, err := service.ClaimConfigurationWriteBackWork(ctx, worker, "writeback-expired", 16); err != nil || len(work) != 0 {
		t.Fatalf("expired proposal became executable: %v", err)
	}
	expiredRead, err := service.GetConfigurationWriteBack(ctx, owner, expired.Proposal.Ref)
	if err != nil || expiredRead.Proposal.State != entity.WriteBackExpired || expiredRead.Proposal.CompletedAt == nil {
		t.Fatalf("expired proposal tombstone: %v", err)
	}
	prepare.Mutation.IdempotencyKey = "writeback-revoke-prepare"
	created, err := service.Execute(ctx, prepare)
	if err != nil {
		t.Fatalf("writeback revoke prepare: %v", err)
	}
	revoked := created.ConfigurationWriteBack
	approve.Mutation.IdempotencyKey = "writeback-revoke-approve"
	approve.Mutation.ExpectedVersion = &revoked.Version
	approve.Payload = command.ConfigurationWriteBackInput{ProposalRef: revoked.Ref, ApprovalDigest: revoked.ApprovalDigest}
	if _, err := service.Execute(ctx, approve); err != nil {
		t.Fatalf("writeback revoke approve: %v", err)
	}
	worker.Permission = "platform.configuration-writebacks.work.claim"
	work, err = service.ClaimConfigurationWriteBackWork(ctx, worker, "writeback-revoke", 1)
	if err != nil || len(work) != 1 {
		t.Fatalf("writeback revoke claim: %v", err)
	}
	begin.Lease, begin.Effect = work[0].Lease, entity.WriteBackBranch
	worker.Permission = "platform.configuration-writebacks.work.begin"
	if _, _, err := service.BeginConfigurationWriteBackEffect(ctx, worker, begin); err != nil {
		t.Fatalf("writeback revoke begin: %v", err)
	}
	connection, err := service.GetIntegrationConnection(ctx, owner, revoked.ConnectionRef)
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := service.Execute(ctx, command.Command{Kind: command.SetConnectionEnabled, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "writeback-disable", ExpectedVersion: &connection.Version}, Payload: command.ConnectionInput{Ref: connection.Ref, Enabled: false}})
	if err != nil || disabled.Connection.Enabled {
		t.Fatalf("unknown effect blocked credential authority withdrawal: %v", err)
	}
	view, err = service.GetConfigurationWriteBack(ctx, owner, revoked.Ref)
	if err != nil || view.Proposal.State != entity.WriteBackUnknown || view.Proposal.FailureCode != "AUTHORITY_CHANGED" {
		t.Fatalf("withdrawal lost unknown intent: %v", err)
	}
	worker.Permission = "platform.configuration-writebacks.work.complete"
	complete = port.ConfigurationWriteBackEffectInput{Lease: begin.Lease, Effect: entity.WriteBackBranch, CandidateCommitSHA: begin.CandidateCommitSHA, ContentSHA256: revoked.ProposedContentSHA256}
	if _, err := service.CompleteConfigurationWriteBackEffect(ctx, worker, complete); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("revoked old claim completed: %v", err)
	}
	worker.Permission = "platform.configuration-writebacks.work.claim"
	work, err = service.ClaimConfigurationWriteBackWork(ctx, worker, "writeback-revoked-readback", 1)
	if err != nil || len(work) != 1 || work[0].Mode != "RECOVER_READ_ONLY" {
		t.Fatalf("revoked effect lost readonly recovery: %v", err)
	}
	complete.Lease = work[0].Lease
	worker.Permission = "platform.configuration-writebacks.work.complete"
	recovered, err := service.CompleteConfigurationWriteBackEffect(ctx, worker, complete)
	if err != nil || recovered.State != entity.WriteBackFailed || recovered.BranchConfirmedAt == nil || recovered.PullRequestRef != "" {
		t.Fatalf("revoked branch receipt authorized PR: %v", err)
	}
	enabled, err := service.Execute(ctx, command.Command{Kind: command.SetConnectionEnabled, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "writeback-enable", ExpectedVersion: &disabled.Connection.Version}, Payload: command.ConnectionInput{Ref: connection.Ref, Enabled: true}})
	if err != nil || !enabled.Connection.Enabled {
		t.Fatalf("restore isolated connection fixture: %v", err)
	}
	// Возвращаем детерминированную provider-readiness fixture, не выполняя внешний Test.
	if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.integration_connections SET state='CONNECTED' WHERE ref=$1`, connection.Ref); err != nil {
		t.Fatal(err)
	}
}
