package grpc

import (
	"context"

	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	grpcruntime "google.golang.org/grpc"
)

var _ interactionsv1.InteractionHubServiceServer = (*Server)(nil)

type interactionService interface {
	CreateConversationThread(context.Context) error
	RecordConversationMessage(context.Context) error
	GetConversationThread(context.Context) error
	ListConversationMessages(context.Context) error
	RequestFeedback(context.Context) error
	RequestApproval(context.Context) error
	RequestHumanGate(context.Context) error
	RecordInteractionResponse(context.Context) error
	CancelInteractionRequest(context.Context) error
	ExpireInteractionRequests(context.Context) error
	GetInteractionRequest(context.Context) error
	ListInteractionRequests(context.Context) error
	RequestNotification(context.Context) error
	UpsertSubscription(context.Context) error
	DisableSubscription(context.Context) error
	ListSubscriptions(context.Context) error
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

func (s *Server) CreateConversationThread(ctx context.Context, _ *interactionsv1.CreateConversationThreadRequest) (*interactionsv1.ConversationThreadResponse, error) {
	return emptyResponse[interactionsv1.ConversationThreadResponse](ctx, s.service.CreateConversationThread)
}

func (s *Server) RecordConversationMessage(ctx context.Context, _ *interactionsv1.RecordConversationMessageRequest) (*interactionsv1.ConversationMessageResponse, error) {
	return emptyResponse[interactionsv1.ConversationMessageResponse](ctx, s.service.RecordConversationMessage)
}

func (s *Server) GetConversationThread(ctx context.Context, _ *interactionsv1.GetConversationThreadRequest) (*interactionsv1.ConversationThreadResponse, error) {
	return emptyResponse[interactionsv1.ConversationThreadResponse](ctx, s.service.GetConversationThread)
}

func (s *Server) ListConversationMessages(ctx context.Context, _ *interactionsv1.ListConversationMessagesRequest) (*interactionsv1.ListConversationMessagesResponse, error) {
	return emptyResponse[interactionsv1.ListConversationMessagesResponse](ctx, s.service.ListConversationMessages)
}

func (s *Server) RequestFeedback(ctx context.Context, _ *interactionsv1.RequestFeedbackRequest) (*interactionsv1.InteractionRequestResponse, error) {
	return emptyResponse[interactionsv1.InteractionRequestResponse](ctx, s.service.RequestFeedback)
}

func (s *Server) RequestApproval(ctx context.Context, _ *interactionsv1.RequestApprovalRequest) (*interactionsv1.InteractionRequestResponse, error) {
	return emptyResponse[interactionsv1.InteractionRequestResponse](ctx, s.service.RequestApproval)
}

func (s *Server) RequestHumanGate(ctx context.Context, _ *interactionsv1.RequestHumanGateRequest) (*interactionsv1.InteractionRequestResponse, error) {
	return emptyResponse[interactionsv1.InteractionRequestResponse](ctx, s.service.RequestHumanGate)
}

func (s *Server) RecordInteractionResponse(ctx context.Context, _ *interactionsv1.RecordInteractionResponseRequest) (*interactionsv1.InteractionResponseResponse, error) {
	return emptyResponse[interactionsv1.InteractionResponseResponse](ctx, s.service.RecordInteractionResponse)
}

func (s *Server) CancelInteractionRequest(ctx context.Context, _ *interactionsv1.CancelInteractionRequestRequest) (*interactionsv1.InteractionRequestResponse, error) {
	return emptyResponse[interactionsv1.InteractionRequestResponse](ctx, s.service.CancelInteractionRequest)
}

func (s *Server) ExpireInteractionRequests(ctx context.Context, _ *interactionsv1.ExpireInteractionRequestsRequest) (*interactionsv1.ExpireInteractionRequestsResponse, error) {
	return emptyResponse[interactionsv1.ExpireInteractionRequestsResponse](ctx, s.service.ExpireInteractionRequests)
}

func (s *Server) GetInteractionRequest(ctx context.Context, _ *interactionsv1.GetInteractionRequestRequest) (*interactionsv1.InteractionRequestResponse, error) {
	return emptyResponse[interactionsv1.InteractionRequestResponse](ctx, s.service.GetInteractionRequest)
}

func (s *Server) ListInteractionRequests(ctx context.Context, _ *interactionsv1.ListInteractionRequestsRequest) (*interactionsv1.ListInteractionRequestsResponse, error) {
	return emptyResponse[interactionsv1.ListInteractionRequestsResponse](ctx, s.service.ListInteractionRequests)
}

func (s *Server) RequestNotification(ctx context.Context, _ *interactionsv1.RequestNotificationRequest) (*interactionsv1.NotificationResponse, error) {
	return emptyResponse[interactionsv1.NotificationResponse](ctx, s.service.RequestNotification)
}

func (s *Server) UpsertSubscription(ctx context.Context, _ *interactionsv1.UpsertSubscriptionRequest) (*interactionsv1.SubscriptionResponse, error) {
	return emptyResponse[interactionsv1.SubscriptionResponse](ctx, s.service.UpsertSubscription)
}

func (s *Server) DisableSubscription(ctx context.Context, _ *interactionsv1.DisableSubscriptionRequest) (*interactionsv1.SubscriptionResponse, error) {
	return emptyResponse[interactionsv1.SubscriptionResponse](ctx, s.service.DisableSubscription)
}

func (s *Server) ListSubscriptions(ctx context.Context, _ *interactionsv1.ListSubscriptionsRequest) (*interactionsv1.ListSubscriptionsResponse, error) {
	return emptyResponse[interactionsv1.ListSubscriptionsResponse](ctx, s.service.ListSubscriptions)
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
