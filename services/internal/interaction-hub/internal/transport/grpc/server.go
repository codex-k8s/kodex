package grpc

import (
	"context"

	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	interactionservice "github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/transport/grpc/casters"
	grpcruntime "google.golang.org/grpc"
)

var _ interactionsv1.InteractionHubServiceServer = (*Server)(nil)

type interactionService interface {
	CreateConversationThread(context.Context, interactionservice.CreateConversationThreadInput) (entity.ConversationThread, error)
	RecordConversationMessage(context.Context, interactionservice.RecordConversationMessageInput) (entity.ConversationMessage, error)
	GetConversationThread(context.Context, interactionservice.GetConversationThreadInput) (entity.ConversationThread, error)
	ListConversationMessages(context.Context, interactionservice.ListConversationMessagesInput) ([]entity.ConversationMessage, value.PageResult, error)
	RequestFeedback(context.Context, interactionservice.RequestFeedbackInput) (entity.InteractionRequest, error)
	RequestApproval(context.Context, interactionservice.RequestApprovalInput) (entity.InteractionRequest, error)
	RequestHumanGate(context.Context, interactionservice.RequestHumanGateInput) (entity.InteractionRequest, error)
	RecordInteractionResponse(context.Context, interactionservice.RecordInteractionResponseInput) (entity.InteractionRequest, entity.InteractionResponse, error)
	CancelInteractionRequest(context.Context, interactionservice.CancelInteractionRequestInput) (entity.InteractionRequest, error)
	ExpireInteractionRequests(context.Context, interactionservice.ExpireInteractionRequestsInput) (interactionservice.ExpireInteractionRequestsResult, error)
	GetInteractionRequest(context.Context, interactionservice.GetInteractionRequestInput) (entity.InteractionRequest, error)
	ListInteractionRequests(context.Context, interactionservice.ListInteractionRequestsInput) ([]entity.InteractionRequest, value.PageResult, error)
	RequestNotification(context.Context, interactionservice.RequestNotificationInput) (entity.Notification, error)
	UpsertSubscription(context.Context, interactionservice.UpsertSubscriptionInput) (entity.Subscription, error)
	DisableSubscription(context.Context, interactionservice.DisableSubscriptionInput) (entity.Subscription, error)
	ListSubscriptions(context.Context, interactionservice.ListSubscriptionsInput) ([]entity.Subscription, value.PageResult, error)
	PlanDelivery(context.Context) error
	RecordDeliveryResult(context.Context) error
	RecordChannelCallback(context.Context) error
	GetDeliveryStatus(context.Context) error
}

// Server implements the generated InteractionHubServiceServer contract.
type Server struct {
	interactionsv1.UnimplementedInteractionHubServiceServer
	service interactionService
}

// NewServer creates an interaction-hub gRPC transport around domain use cases.
func NewServer(service interactionService) *Server {
	if service == nil {
		panic("interaction-hub grpc service is required")
	}
	return &Server{service: service}
}

// RegisterInteractionHubService registers interaction-hub gRPC handlers.
func RegisterInteractionHubService(registrar grpcruntime.ServiceRegistrar, service interactionService) {
	interactionsv1.RegisterInteractionHubServiceServer(registrar, NewServer(service))
}

func (s *Server) CreateConversationThread(ctx context.Context, request *interactionsv1.CreateConversationThreadRequest) (*interactionsv1.ConversationThreadResponse, error) {
	return commandResponse(ctx, request, casters.CreateConversationThreadInput, s.service.CreateConversationThread, casters.ConversationThreadResponse)
}

func (s *Server) RecordConversationMessage(ctx context.Context, request *interactionsv1.RecordConversationMessageRequest) (*interactionsv1.ConversationMessageResponse, error) {
	return commandResponse(ctx, request, casters.RecordConversationMessageInput, s.service.RecordConversationMessage, casters.ConversationMessageResponse)
}

func (s *Server) GetConversationThread(ctx context.Context, request *interactionsv1.GetConversationThreadRequest) (*interactionsv1.ConversationThreadResponse, error) {
	return commandResponse(ctx, request, casters.GetConversationThreadInput, s.service.GetConversationThread, casters.ConversationThreadResponse)
}

func (s *Server) ListConversationMessages(ctx context.Context, request *interactionsv1.ListConversationMessagesRequest) (*interactionsv1.ListConversationMessagesResponse, error) {
	input, err := casters.ListConversationMessagesInput(request)
	if err != nil {
		return nil, err
	}
	messages, page, err := s.service.ListConversationMessages(ctx, input)
	if err != nil {
		return nil, err
	}
	return casters.ListConversationMessagesResponse(messages, page), nil
}

func (s *Server) RequestFeedback(ctx context.Context, request *interactionsv1.RequestFeedbackRequest) (*interactionsv1.InteractionRequestResponse, error) {
	return commandResponse(ctx, request, casters.RequestFeedbackInput, s.service.RequestFeedback, casters.InteractionRequestResponse)
}

func (s *Server) RequestApproval(ctx context.Context, request *interactionsv1.RequestApprovalRequest) (*interactionsv1.InteractionRequestResponse, error) {
	return commandResponse(ctx, request, casters.RequestApprovalInput, s.service.RequestApproval, casters.InteractionRequestResponse)
}

func (s *Server) RequestHumanGate(ctx context.Context, request *interactionsv1.RequestHumanGateRequest) (*interactionsv1.InteractionRequestResponse, error) {
	return commandResponse(ctx, request, casters.RequestHumanGateInput, s.service.RequestHumanGate, casters.InteractionRequestResponse)
}

func (s *Server) RecordInteractionResponse(ctx context.Context, request *interactionsv1.RecordInteractionResponseRequest) (*interactionsv1.InteractionResponseResponse, error) {
	input, err := casters.RecordInteractionResponseInput(request)
	if err != nil {
		return nil, err
	}
	interactionRequest, response, err := s.service.RecordInteractionResponse(ctx, input)
	if err != nil {
		return nil, err
	}
	return casters.InteractionResponseResponse(interactionRequest, response), nil
}

func (s *Server) CancelInteractionRequest(ctx context.Context, request *interactionsv1.CancelInteractionRequestRequest) (*interactionsv1.InteractionRequestResponse, error) {
	return commandResponse(ctx, request, casters.CancelInteractionRequestInput, s.service.CancelInteractionRequest, casters.InteractionRequestResponse)
}

func (s *Server) ExpireInteractionRequests(ctx context.Context, request *interactionsv1.ExpireInteractionRequestsRequest) (*interactionsv1.ExpireInteractionRequestsResponse, error) {
	return commandResponse(ctx, request, casters.ExpireInteractionRequestsInput, s.service.ExpireInteractionRequests, casters.ExpireInteractionRequestsResponse)
}

func (s *Server) GetInteractionRequest(ctx context.Context, request *interactionsv1.GetInteractionRequestRequest) (*interactionsv1.InteractionRequestResponse, error) {
	return commandResponse(ctx, request, casters.GetInteractionRequestInput, s.service.GetInteractionRequest, casters.InteractionRequestResponse)
}

func (s *Server) ListInteractionRequests(ctx context.Context, request *interactionsv1.ListInteractionRequestsRequest) (*interactionsv1.ListInteractionRequestsResponse, error) {
	input, err := casters.ListInteractionRequestsInput(request)
	if err != nil {
		return nil, err
	}
	requests, page, err := s.service.ListInteractionRequests(ctx, input)
	if err != nil {
		return nil, err
	}
	return casters.ListInteractionRequestsResponse(requests, page), nil
}

func (s *Server) RequestNotification(ctx context.Context, request *interactionsv1.RequestNotificationRequest) (*interactionsv1.NotificationResponse, error) {
	return commandResponse(ctx, request, casters.RequestNotificationInput, s.service.RequestNotification, casters.NotificationResponse)
}

func (s *Server) UpsertSubscription(ctx context.Context, request *interactionsv1.UpsertSubscriptionRequest) (*interactionsv1.SubscriptionResponse, error) {
	return commandResponse(ctx, request, casters.UpsertSubscriptionInput, s.service.UpsertSubscription, casters.SubscriptionResponse)
}

func (s *Server) DisableSubscription(ctx context.Context, request *interactionsv1.DisableSubscriptionRequest) (*interactionsv1.SubscriptionResponse, error) {
	return commandResponse(ctx, request, casters.DisableSubscriptionInput, s.service.DisableSubscription, casters.SubscriptionResponse)
}

func (s *Server) ListSubscriptions(ctx context.Context, request *interactionsv1.ListSubscriptionsRequest) (*interactionsv1.ListSubscriptionsResponse, error) {
	input, err := casters.ListSubscriptionsInput(request)
	if err != nil {
		return nil, err
	}
	subscriptions, page, err := s.service.ListSubscriptions(ctx, input)
	if err != nil {
		return nil, err
	}
	return casters.ListSubscriptionsResponse(subscriptions, page), nil
}

func (s *Server) PlanDelivery(ctx context.Context, _ *interactionsv1.PlanDeliveryRequest) (*interactionsv1.DeliveryAttemptResponse, error) {
	return emptyResponse[interactionsv1.DeliveryAttemptResponse](ctx, s.service.PlanDelivery)
}

func (s *Server) RecordDeliveryResult(ctx context.Context, _ *interactionsv1.RecordDeliveryResultRequest) (*interactionsv1.DeliveryAttemptResponse, error) {
	return emptyResponse[interactionsv1.DeliveryAttemptResponse](ctx, s.service.RecordDeliveryResult)
}

func (s *Server) RecordChannelCallback(ctx context.Context, _ *interactionsv1.RecordChannelCallbackRequest) (*interactionsv1.ChannelCallbackResponse, error) {
	return emptyResponse[interactionsv1.ChannelCallbackResponse](ctx, s.service.RecordChannelCallback)
}

func (s *Server) GetDeliveryStatus(ctx context.Context, _ *interactionsv1.GetDeliveryStatusRequest) (*interactionsv1.DeliveryStatusResponse, error) {
	return emptyResponse[interactionsv1.DeliveryStatusResponse](ctx, s.service.GetDeliveryStatus)
}

func emptyResponse[Response any](ctx context.Context, call func(context.Context) error) (*Response, error) {
	if err := call(ctx); err != nil {
		return nil, err
	}
	return new(Response), nil
}

func commandResponse[Request any, Input any, Output any, Response any](
	ctx context.Context,
	request *Request,
	decode func(*Request) (Input, error),
	call func(context.Context, Input) (Output, error),
	encode func(Output) *Response,
) (*Response, error) {
	input, err := decode(request)
	if err != nil {
		return nil, err
	}
	output, err := call(ctx, input)
	if err != nil {
		return nil, err
	}
	return encode(output), nil
}
