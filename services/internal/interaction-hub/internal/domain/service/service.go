package service

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/errs"
	interactionrepo "github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/repository/interaction"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
)

// Service coordinates interaction-hub domain use cases.
type Service struct {
	repository interactionrepo.Repository
}

// New creates a domain service with injected persistence.
func New(repository interactionrepo.Repository) *Service {
	return &Service{repository: repository}
}

// Ready reports whether the composed domain dependencies are available.
func (s *Service) Ready() bool {
	return s != nil && s.repository != nil && s.repository.Ready()
}

func (s *Service) CreateConversationThread(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationCreateConversationThread)
}

func (s *Service) RecordConversationMessage(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationRecordConversationMessage)
}

func (s *Service) GetConversationThread(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationGetConversationThread)
}

func (s *Service) ListConversationMessages(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationListConversationMessages)
}

func (s *Service) RequestFeedback(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationRequestFeedback)
}

func (s *Service) RequestApproval(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationRequestApproval)
}

func (s *Service) RequestHumanGate(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationRequestHumanGate)
}

func (s *Service) RecordInteractionResponse(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationRecordInteractionResponse)
}

func (s *Service) CancelInteractionRequest(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationCancelInteractionRequest)
}

func (s *Service) ExpireInteractionRequests(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationExpireInteractionRequests)
}

func (s *Service) GetInteractionRequest(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationGetInteractionRequest)
}

func (s *Service) ListInteractionRequests(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationListInteractionRequests)
}

func (s *Service) RequestNotification(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationRequestNotification)
}

func (s *Service) UpsertSubscription(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationUpsertSubscription)
}

func (s *Service) DisableSubscription(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationDisableSubscription)
}

func (s *Service) ListSubscriptions(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationListSubscriptions)
}

func (s *Service) PlanDelivery(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationPlanDelivery)
}

func (s *Service) RecordDeliveryResult(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationRecordDeliveryResult)
}

func (s *Service) RecordChannelCallback(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationRecordChannelCallback)
}

func (s *Service) GetDeliveryStatus(ctx context.Context) error {
	return s.backlog(ctx, enum.OperationGetDeliveryStatus)
}

func (s *Service) backlog(ctx context.Context, operation enum.Operation) error {
	if s == nil || s.repository == nil || !s.repository.Ready() {
		return errs.ErrUnavailable
	}
	if !operation.Valid() {
		return errs.ErrInvalidArgument
	}
	if err := s.repository.RecordBacklogOperation(ctx, operation); err != nil {
		return err
	}
	return errs.ErrNotImplemented
}
