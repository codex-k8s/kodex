package integrations

import (
	"context"
	"errors"
	"fmt"
	"time"

	domain "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/integrations"
	"github.com/jackc/pgx/v5"
)

func (repo *Repository) ClaimApprovalDelivery(ctx context.Context, approvalID int64, leaseOwner string, now time.Time, leaseDuration time.Duration) (domain.ApprovalDelivery, bool, error) {
	if approvalID <= 0 || leaseOwner == "" || leaseDuration <= 0 {
		return domain.ApprovalDelivery{}, false, domain.ErrApprovalBinding
	}
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return domain.ApprovalDelivery{}, false, fmt.Errorf("begin approval delivery claim: %w", err)
	}
	defer rollback(ctx, tx)
	var claimedID int64
	err = tx.QueryRow(ctx, query("approval_delivery__claim.sql"), approvalID, leaseOwner, now, now.Add(leaseDuration)).Scan(&claimedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ApprovalDelivery{}, false, nil
	}
	if err != nil {
		return domain.ApprovalDelivery{}, false, fmt.Errorf("claim approval delivery: %w", err)
	}
	var delivery domain.ApprovalDelivery
	err = tx.QueryRow(ctx, query("approval_delivery__get.sql"), claimedID, leaseOwner).Scan(
		&delivery.ApprovalID, &delivery.ApprovalPublicID, &delivery.InvocationPublicID,
		&delivery.CapabilityKey, &delivery.ConnectionPublicID,
		&delivery.Arguments.Namespace, &delivery.Arguments.WorkloadKind, &delivery.Arguments.WorkloadName,
		&delivery.ArgumentsHash, &delivery.ApprovalBindingHash, &delivery.RiskClass,
		&delivery.ApproverUserID, &delivery.ApproverUserName,
		&delivery.WorkspaceScope, &delivery.SessionScope,
		&delivery.ChannelID, &delivery.RootPostID, &delivery.PostID,
		&delivery.ExpiresAt, &delivery.DeliveryLeaseOwner,
	)
	if err != nil {
		return domain.ApprovalDelivery{}, false, fmt.Errorf("read approval delivery: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ApprovalDelivery{}, false, fmt.Errorf("commit approval delivery claim: %w", err)
	}
	return delivery, true, nil
}

func (repo *Repository) CompleteApprovalDelivery(ctx context.Context, approvalID int64, leaseOwner string, postID string, now time.Time) error {
	command, err := repo.db.Exec(ctx, query("approval_delivery__complete.sql"), approvalID, leaseOwner, postID, now)
	if err != nil {
		return fmt.Errorf("complete approval delivery: %w", err)
	}
	if command.RowsAffected() != 1 {
		return domain.ErrApprovalBinding
	}
	return nil
}

func (repo *Repository) ReleaseApprovalDelivery(ctx context.Context, approvalID int64, leaseOwner string, reasonCode string, now time.Time) error {
	command, err := repo.db.Exec(ctx, query("approval_delivery__release.sql"), approvalID, leaseOwner, reasonCode, now)
	if err != nil {
		return fmt.Errorf("release approval delivery: %w", err)
	}
	if command.RowsAffected() != 1 {
		return domain.ErrApprovalBinding
	}
	return nil
}

type lockedApproval struct {
	id                int64
	invocationID      int64
	state             domain.InvocationStatus
	bindingHash       string
	approverUserID    string
	approverUserName  string
	expiresAt         time.Time
	channelID         string
	rootPostID        string
	postID            string
	invocationState   domain.InvocationStatus
	correlationID     string
	installationScope string
	workspaceScope    string
	sessionScope      string
}

func (repo *Repository) DecideApproval(ctx context.Context, input domain.ApprovalDecisionInput) (domain.Invocation, error) {
	if input.Decision != domain.ApprovalDecisionApprove && input.Decision != domain.ApprovalDecisionReject {
		return domain.Invocation{}, domain.ErrInvalidInput
	}
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return domain.Invocation{}, fmt.Errorf("begin integration approval decision: %w", err)
	}
	defer rollback(ctx, tx)
	txRepo := newTransactionalRepository(tx)
	var locked lockedApproval
	err = tx.QueryRow(ctx, query("approval__lock.sql"), input.ApprovalPublicID).Scan(
		&locked.id, &locked.invocationID, &locked.state, &locked.bindingHash,
		&locked.approverUserID, &locked.approverUserName, &locked.expiresAt,
		&locked.channelID, &locked.rootPostID, &locked.postID,
		&locked.invocationState, &locked.correlationID, &locked.installationScope,
		&locked.workspaceScope, &locked.sessionScope,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Invocation{}, domain.ErrApprovalBinding
	}
	if err != nil {
		return domain.Invocation{}, fmt.Errorf("lock integration approval: %w", err)
	}
	if locked.bindingHash != input.ApprovalBindingHash || locked.channelID != input.ChannelID ||
		locked.postID == "" || locked.postID != input.PostID {
		return domain.Invocation{}, domain.ErrApprovalBinding
	}
	if locked.approverUserID == "" || locked.approverUserID != input.ActorUserID {
		return domain.Invocation{}, domain.ErrApprovalActor
	}
	desired := domain.InvocationStatusApproved
	reason := "approval.approved"
	if input.Decision == domain.ApprovalDecisionReject {
		desired = domain.InvocationStatusRejected
		reason = "approval.rejected"
	}
	if locked.state != domain.InvocationStatusPending {
		if locked.state != desired || locked.invocationState != desired {
			return domain.Invocation{}, domain.ErrApprovalTerminal
		}
		invocation, err := txRepo.invocationByID(ctx, locked.invocationID)
		if err != nil {
			return domain.Invocation{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Invocation{}, fmt.Errorf("commit repeated integration approval: %w", err)
		}
		return invocation, nil
	}
	if !locked.expiresAt.After(input.Now) {
		if err := txRepo.applyApprovalDecision(ctx, locked, domain.InvocationStatusExpired, input, "approval.expired"); err != nil {
			return domain.Invocation{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Invocation{}, fmt.Errorf("commit expired integration approval: %w", err)
		}
		return domain.Invocation{}, domain.ErrApprovalTerminal
	}
	if err := txRepo.applyApprovalDecision(ctx, locked, desired, input, reason); err != nil {
		return domain.Invocation{}, err
	}
	invocation, err := txRepo.invocationByID(ctx, locked.invocationID)
	if err != nil {
		return domain.Invocation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Invocation{}, fmt.Errorf("commit integration approval: %w", err)
	}
	return invocation, nil
}

func (repo *Repository) applyApprovalDecision(ctx context.Context, locked lockedApproval, desired domain.InvocationStatus, input domain.ApprovalDecisionInput, reasonCode string) error {
	approvalCommand, err := repo.db.Exec(ctx, query("approval__decide.sql"),
		locked.id, desired, input.ActorUserID, input.ActorUserName, input.Now, reasonCode,
	)
	if err != nil {
		return fmt.Errorf("update integration approval: %w", err)
	}
	if approvalCommand.RowsAffected() != 1 {
		return domain.ErrApprovalTerminal
	}
	invocationCommand, err := repo.db.Exec(ctx, query("invocation__decide.sql"),
		locked.invocationID, desired, reasonCode, input.Now,
	)
	if err != nil {
		return fmt.Errorf("update integration invocation decision: %w", err)
	}
	if invocationCommand.RowsAffected() != 1 {
		return domain.ErrApprovalTerminal
	}
	outcome := string(desired)
	return repo.appendAudit(ctx, auditInput{
		EventType: "integration.approval.decided", ActorUserID: input.ActorUserID, ActorUser: input.ActorUserName,
		ResourceType: "approval_request", ResourceName: input.ApprovalPublicID,
		Summary: "Сохранено решение по опасной capability.", CorrelationID: locked.correlationID,
		InstallationScope: locked.installationScope, WorkspaceScope: locked.workspaceScope,
		SessionScope: locked.sessionScope, Outcome: outcome, ReasonCode: reasonCode,
		Metadata: auditMetadata{ApprovalID: input.ApprovalPublicID}, Now: input.Now,
	})
}
