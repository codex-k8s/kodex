package repository

import (
	"context"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

// RestoreCoordinationStore владеет устойчивой координацией восстановления.
type RestoreCoordinationStore interface {
	Prepare(context.Context, model.PrepareRestoreCommand) (model.RestoreState, error)
	Load(context.Context) (model.RestoreState, error)
	EnsureIssuance(
		context.Context,
		string,
		model.RestoreIssuanceRecord,
	) (model.RestoreIssuanceRecord, error)
	RecordDelivery(
		context.Context,
		string,
		model.RestoreDeliveryRecord,
	) (model.RestoreState, error)
	SaveDirective(
		context.Context,
		string,
		model.RestoreDirectiveRecord,
	) (model.RestoreDirectiveRecord, error)
	RecordACK(
		context.Context,
		string,
		model.RestoreACKRecord,
	) (model.RestoreState, model.RestoreACKRecord, error)
	AuthorizeOperator(
		context.Context,
		model.RestoreOperatorAuthorizationRecord,
	) error
	Complete(context.Context, model.CompleteRestoreCommand) (model.RestoreState, error)
	CoordinationReady(context.Context) error
}

// RestoreOperatorCredentialVerifier проверяет projected ServiceAccount token
// через Kubernetes TokenReview с exact audience и subject.
type RestoreOperatorCredentialVerifier interface {
	VerifyOperatorCredential(
		context.Context,
		string,
		string,
	) (model.RestoreOperatorCredential, error)
}

// RestoreEvidenceVerifier проверяет independently signed PITR evidence.
type RestoreEvidenceVerifier interface {
	VerifyCompletedEvidence(
		context.Context,
		model.RestoreState,
	) (model.RestoreCompletionEvidence, error)
}

// RestoreFenceStore применяет и проверяет рабочий fence восстановления.
type RestoreFenceStore interface {
	ApplyRestoreFence(context.Context, model.RestoreState) error
	RestoreFenceReady(context.Context, model.RestoreState) error
}

// RestoreCredentialPublisher доставляет role-bound credential восстановления.
type RestoreCredentialPublisher interface {
	PublishRoleCredential(
		context.Context,
		string,
		string,
	) (model.RestoreDeliveryRecord, error)
	PublisherReady(context.Context) error
}
