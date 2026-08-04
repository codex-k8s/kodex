package resource

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

const runtimeRetentionPolicyID = "prototype-testing-v1"

type runtimeRetentionTransaction interface {
	GetResourceRetentionPolicyForUpdate(context.Context, string, string) (domainrepo.ResourceRetentionPolicy, error)
	GetResourceRetentionPolicyVersionForUpdate(context.Context, string, string, string, uint64) (domainrepo.ResourceRetentionPolicy, error)
	InsertResourceRetentionPolicy(context.Context, domainrepo.ResourceRetentionPolicy, string, string, string, string) error
	RetireResourceRetentionPolicy(context.Context, domainrepo.ResourceRetentionPolicy, time.Time) error
	GetActiveRuntimeRetentionHoldForUpdate(context.Context, string, string) (domainrepo.RuntimeRetentionHold, error)
	GetRuntimeRetentionHoldForUpdate(context.Context, string) (domainrepo.RuntimeRetentionHold, error)
	InsertRuntimeRetentionHold(context.Context, domainrepo.RuntimeRetentionHold, string, string) error
	ReleaseRuntimeRetentionHold(context.Context, domainrepo.RuntimeRetentionHold, string, time.Time) error
}

func retentionTransaction(tx domainrepo.Transaction) (runtimeRetentionTransaction, error) {
	retention, ok := tx.(runtimeRetentionTransaction)
	if !ok {
		return nil, errs.ErrInternal
	}
	return retention, nil
}

func validateRuntimeRetentionOperator(principal value.Principal, permission string) error {
	if err := authorize(principal, permission); err != nil {
		return err
	}
	if principal.CallerWorkload != controlAPIGatewayWorkload ||
		principal.CallerSPIFFEID != controlAPIGatewaySPIFFEID {
		return errs.ErrPermissionDenied
	}
	return nil
}

// GetResourceRetentionPolicy возвращает только current owner policy. Missing
// или retired policy не подменяется process-local default.
func (service *Service) GetResourceRetentionPolicy(
	ctx context.Context,
	principal value.Principal,
) (domainrepo.ResourceRetentionPolicy, error) {
	if err := validateRuntimeRetentionOperator(principal, permissionRuntimeRetentionRead); err != nil {
		return domainrepo.ResourceRetentionPolicy{}, err
	}
	var result domainrepo.ResourceRetentionPolicy
	err := service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID,
		ActorID: principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		var err error
		result, err = tx.GetCurrentResourceRetentionPolicy(
			ctx, principal.OrganizationID, principal.ProjectID,
		)
		return err
	})
	return result, err
}

// SetResourceRetentionPolicy атомарно retires current version и создаёт
// следующую. Уже pinned executions не переписываются.
func (service *Service) SetResourceRetentionPolicy(
	ctx context.Context,
	input ResourceRetentionPolicyInput,
) (domainrepo.ResourceRetentionPolicy, error) {
	if err := validateRuntimeRetentionOperator(input.Principal, permissionRuntimeRetentionManage); err != nil {
		return domainrepo.ResourceRetentionPolicy{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || input.ExpectedVersion == 0 ||
		input.PVCRetentionSeconds < 86400 || input.PVCRetentionSeconds > 2592000 ||
		input.ArchiveRetentionSeconds < 7776000 || input.ArchiveRetentionSeconds > 315360000 ||
		value.ValidateStableKey(input.ReasonCode) != nil {
		return domainrepo.ResourceRetentionPolicy{}, errs.ErrInvalidInput
	}
	requestHash, err := semanticCommandHash(input.Principal, struct {
		ExpectedVersion, PVCRetentionSeconds, ArchiveRetentionSeconds uint64
		ReasonCode                                                    string
	}{input.ExpectedVersion, input.PVCRetentionSeconds, input.ArchiveRetentionSeconds, input.ReasonCode})
	if err != nil {
		return domainrepo.ResourceRetentionPolicy{}, errs.ErrInvalidInput
	}
	var current, result domainrepo.ResourceRetentionPolicy
	err = service.withLifecycleReceipt(ctx, input.Principal, input.IdempotencyKey,
		"set_resource_retention_policy", requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			retention, err := retentionTransaction(tx)
			if err != nil {
				return 0, err
			}
			current, err = retention.GetResourceRetentionPolicyForUpdate(
				ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
			)
			if err != nil {
				return 0, err
			}
			if current.ID != runtimeRetentionPolicyID {
				return 0, errs.ErrStateConflict
			}
			if current.Version == input.ExpectedVersion {
				return lifecycleReceiptApply, nil
			}
			if current.Version == input.ExpectedVersion+1 {
				return lifecycleReceiptReplay, nil
			}
			return 0, errs.ErrStateConflict
		},
		func() error {
			if result.ID != current.ID || result.Version != current.Version ||
				result.PVCRetentionSeconds != current.PVCRetentionSeconds ||
				result.ArchiveRetentionSeconds != current.ArchiveRetentionSeconds {
				return errs.ErrStateConflict
			}
			return nil
		},
		func(tx domainrepo.Transaction) error {
			retention, err := retentionTransaction(tx)
			if err != nil {
				return err
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			if err := retention.RetireResourceRetentionPolicy(ctx, current, now); err != nil {
				return err
			}
			result = domainrepo.ResourceRetentionPolicy{
				ID: current.ID, Version: current.Version + 1,
				PVCRetentionSeconds:     input.PVCRetentionSeconds,
				ArchiveRetentionSeconds: input.ArchiveRetentionSeconds,
				EffectiveFrom:           now,
			}
			if err := retention.InsertResourceRetentionPolicy(
				ctx, result, input.Principal.ActorID, input.ReasonCode,
				hashString(input.IdempotencyKey), requestHash,
			); err != nil {
				return err
			}
			return service.appendLifecycleAudit(ctx, tx, input.Principal,
				"set_resource_retention_policy", input.Principal.ProjectID,
				"RESOURCE_RETENTION_POLICY", result.Version, now)
		},
	)
	return result, err
}

func (service *Service) RetireResourceRetentionPolicy(
	ctx context.Context,
	input ResourceRetentionPolicyInput,
) (domainrepo.ResourceRetentionPolicy, error) {
	if err := validateRuntimeRetentionOperator(input.Principal, permissionRuntimeRetentionManage); err != nil {
		return domainrepo.ResourceRetentionPolicy{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || input.ExpectedVersion == 0 ||
		input.PVCRetentionSeconds != 0 || input.ArchiveRetentionSeconds != 0 ||
		value.ValidateStableKey(input.ReasonCode) != nil {
		return domainrepo.ResourceRetentionPolicy{}, errs.ErrInvalidInput
	}
	requestHash, err := semanticCommandHash(input.Principal, struct {
		ExpectedVersion uint64
		ReasonCode      string
	}{input.ExpectedVersion, input.ReasonCode})
	if err != nil {
		return domainrepo.ResourceRetentionPolicy{}, errs.ErrInvalidInput
	}
	var current, result domainrepo.ResourceRetentionPolicy
	err = service.withLifecycleReceipt(ctx, input.Principal, input.IdempotencyKey,
		"retire_resource_retention_policy", requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			retention, err := retentionTransaction(tx)
			if err != nil {
				return 0, err
			}
			current, err = retention.GetResourceRetentionPolicyVersionForUpdate(
				ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
				runtimeRetentionPolicyID, input.ExpectedVersion,
			)
			if err != nil {
				return 0, err
			}
			if current.RetiredAt.IsZero() {
				return lifecycleReceiptApply, nil
			}
			return lifecycleReceiptReplay, nil
		},
		func() error {
			if result.ID != current.ID || result.Version != current.Version ||
				result.RetiredAt.IsZero() || !result.RetiredAt.Equal(current.RetiredAt) {
				return errs.ErrStateConflict
			}
			return nil
		},
		func(tx domainrepo.Transaction) error {
			retention, err := retentionTransaction(tx)
			if err != nil {
				return err
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			if err := retention.RetireResourceRetentionPolicy(ctx, current, now); err != nil {
				return err
			}
			result = current
			result.RetiredAt = now
			return service.appendLifecycleAudit(ctx, tx, input.Principal,
				"retire_resource_retention_policy", input.Principal.ProjectID,
				"RESOURCE_RETENTION_POLICY", result.Version, now)
		},
	)
	return result, err
}

func (service *Service) HoldRuntimeRetention(
	ctx context.Context,
	input RuntimeRetentionHoldInput,
) (domainrepo.RuntimeRetentionHold, error) {
	if err := validateRuntimeRetentionOperator(input.Principal, permissionRuntimeRetentionManage); err != nil {
		return domainrepo.RuntimeRetentionHold{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.SessionID) != nil || input.ExpectedSessionVersion == 0 ||
		(input.Kind != "MANUAL" && input.Kind != "LEGAL") ||
		value.ValidateStableKey(input.ReasonCode) != nil || input.HoldID != "" ||
		input.ExpectedHoldVersion != 0 {
		return domainrepo.RuntimeRetentionHold{}, errs.ErrInvalidInput
	}
	requestHash, err := semanticCommandHash(input.Principal, struct {
		SessionID              string
		ExpectedSessionVersion uint64
		Kind, ReasonCode       string
	}{input.SessionID, input.ExpectedSessionVersion, input.Kind, input.ReasonCode})
	if err != nil {
		return domainrepo.RuntimeRetentionHold{}, errs.ErrInvalidInput
	}
	var current, result domainrepo.RuntimeRetentionHold
	err = service.withLifecycleReceipt(ctx, input.Principal, input.IdempotencyKey,
		"hold_runtime_retention", requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			session, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
				input.Principal.ProjectID, input.SessionID)
			if err != nil {
				return 0, err
			}
			if session.Kind != enum.KindSession || session.Version != input.ExpectedSessionVersion ||
				session.OwnerActorID != input.Principal.ActorID {
				return 0, errs.ErrStateConflict
			}
			retention, err := retentionTransaction(tx)
			if err != nil {
				return 0, err
			}
			current, err = retention.GetActiveRuntimeRetentionHoldForUpdate(ctx, input.SessionID, input.Kind)
			if err == nil {
				return lifecycleReceiptReplay, nil
			}
			if !errors.Is(err, errs.ErrNotFound) {
				return 0, err
			}
			return lifecycleReceiptApply, nil
		},
		func() error {
			if result.ID != current.ID || result.SessionID != current.SessionID ||
				result.Version != current.Version || result.State != "ACTIVE" {
				return errs.ErrStateConflict
			}
			return nil
		},
		func(tx domainrepo.Transaction) error {
			retention, err := retentionTransaction(tx)
			if err != nil {
				return err
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			result = domainrepo.RuntimeRetentionHold{
				ID: uuid.NewString(), OrganizationID: input.Principal.OrganizationID,
				ProjectID: input.Principal.ProjectID, SessionID: input.SessionID,
				Kind: input.Kind, State: "ACTIVE", Version: 1,
				ActorID: input.Principal.ActorID, ReasonCode: input.ReasonCode,
				CreatedAt: now, UpdatedAt: now,
			}
			if err := retention.InsertRuntimeRetentionHold(
				ctx, result, hashString(input.IdempotencyKey), requestHash,
			); err != nil {
				return err
			}
			return service.appendLifecycleAudit(ctx, tx, input.Principal,
				"hold_runtime_retention", input.SessionID, "SESSION", result.Version, now)
		},
	)
	return result, err
}

func (service *Service) ReleaseRuntimeRetention(
	ctx context.Context,
	input RuntimeRetentionHoldInput,
) (domainrepo.RuntimeRetentionHold, error) {
	if err := validateRuntimeRetentionOperator(input.Principal, permissionRuntimeRetentionManage); err != nil {
		return domainrepo.RuntimeRetentionHold{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.SessionID) != nil || input.ExpectedSessionVersion == 0 ||
		value.ValidateID(input.HoldID) != nil || input.ExpectedHoldVersion == 0 ||
		input.Kind != "" || value.ValidateStableKey(input.ReasonCode) != nil {
		return domainrepo.RuntimeRetentionHold{}, errs.ErrInvalidInput
	}
	requestHash, err := semanticCommandHash(input.Principal, struct {
		SessionID              string
		ExpectedSessionVersion uint64
		HoldID                 string
		ExpectedHoldVersion    uint64
		ReasonCode             string
	}{input.SessionID, input.ExpectedSessionVersion, input.HoldID,
		input.ExpectedHoldVersion, input.ReasonCode})
	if err != nil {
		return domainrepo.RuntimeRetentionHold{}, errs.ErrInvalidInput
	}
	var current, result domainrepo.RuntimeRetentionHold
	err = service.withLifecycleReceipt(ctx, input.Principal, input.IdempotencyKey,
		"release_runtime_retention", requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			session, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
				input.Principal.ProjectID, input.SessionID)
			if err != nil {
				return 0, err
			}
			if session.Kind != enum.KindSession || session.Version != input.ExpectedSessionVersion ||
				session.OwnerActorID != input.Principal.ActorID {
				return 0, errs.ErrStateConflict
			}
			retention, err := retentionTransaction(tx)
			if err != nil {
				return 0, err
			}
			current, err = retention.GetRuntimeRetentionHoldForUpdate(ctx, input.HoldID)
			if err != nil {
				return 0, err
			}
			if current.SessionID != input.SessionID {
				return 0, errs.ErrNotFound
			}
			if current.Version == input.ExpectedHoldVersion && current.State == "ACTIVE" {
				return lifecycleReceiptApply, nil
			}
			if current.Version == input.ExpectedHoldVersion+1 && current.State == "RELEASED" {
				return lifecycleReceiptReplay, nil
			}
			return 0, errs.ErrStateConflict
		},
		func() error {
			if result.ID != current.ID || result.Version != current.Version || result.State != "RELEASED" {
				return errs.ErrStateConflict
			}
			return nil
		},
		func(tx domainrepo.Transaction) error {
			retention, err := retentionTransaction(tx)
			if err != nil {
				return err
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			if err := retention.ReleaseRuntimeRetentionHold(ctx, current, input.ReasonCode, now); err != nil {
				return err
			}
			result = current
			result.State = "RELEASED"
			result.Version++
			result.ReasonCode = input.ReasonCode
			result.UpdatedAt = now
			result.ReleasedAt = now
			return service.appendLifecycleAudit(ctx, tx, input.Principal,
				"release_runtime_retention", input.SessionID, "SESSION", result.Version, now)
		},
	)
	return result, err
}
