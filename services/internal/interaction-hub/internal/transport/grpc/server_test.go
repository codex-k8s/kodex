package grpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestServerRoutesAllStableRPCsToDomainUseCases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		want enum.Operation
		call func(context.Context, *Server) error
	}{
		{name: "CreateConversationThread", want: enum.OperationCreateConversationThread, call: func(ctx context.Context, s *Server) error {
			_, err := s.CreateConversationThread(ctx, &interactionsv1.CreateConversationThreadRequest{})
			return err
		}},
		{name: "RecordConversationMessage", want: enum.OperationRecordConversationMessage, call: func(ctx context.Context, s *Server) error {
			_, err := s.RecordConversationMessage(ctx, &interactionsv1.RecordConversationMessageRequest{})
			return err
		}},
		{name: "GetConversationThread", want: enum.OperationGetConversationThread, call: func(ctx context.Context, s *Server) error {
			_, err := s.GetConversationThread(ctx, &interactionsv1.GetConversationThreadRequest{})
			return err
		}},
		{name: "ListConversationMessages", want: enum.OperationListConversationMessages, call: func(ctx context.Context, s *Server) error {
			_, err := s.ListConversationMessages(ctx, &interactionsv1.ListConversationMessagesRequest{})
			return err
		}},
		{name: "RequestFeedback", want: enum.OperationRequestFeedback, call: func(ctx context.Context, s *Server) error {
			_, err := s.RequestFeedback(ctx, &interactionsv1.RequestFeedbackRequest{})
			return err
		}},
		{name: "RequestApproval", want: enum.OperationRequestApproval, call: func(ctx context.Context, s *Server) error {
			_, err := s.RequestApproval(ctx, &interactionsv1.RequestApprovalRequest{})
			return err
		}},
		{name: "RequestHumanGate", want: enum.OperationRequestHumanGate, call: func(ctx context.Context, s *Server) error {
			_, err := s.RequestHumanGate(ctx, &interactionsv1.RequestHumanGateRequest{})
			return err
		}},
		{name: "RecordInteractionResponse", want: enum.OperationRecordInteractionResponse, call: func(ctx context.Context, s *Server) error {
			_, err := s.RecordInteractionResponse(ctx, &interactionsv1.RecordInteractionResponseRequest{})
			return err
		}},
		{name: "CancelInteractionRequest", want: enum.OperationCancelInteractionRequest, call: func(ctx context.Context, s *Server) error {
			_, err := s.CancelInteractionRequest(ctx, &interactionsv1.CancelInteractionRequestRequest{})
			return err
		}},
		{name: "ExpireInteractionRequests", want: enum.OperationExpireInteractionRequests, call: func(ctx context.Context, s *Server) error {
			_, err := s.ExpireInteractionRequests(ctx, &interactionsv1.ExpireInteractionRequestsRequest{})
			return err
		}},
		{name: "GetInteractionRequest", want: enum.OperationGetInteractionRequest, call: func(ctx context.Context, s *Server) error {
			_, err := s.GetInteractionRequest(ctx, &interactionsv1.GetInteractionRequestRequest{})
			return err
		}},
		{name: "ListInteractionRequests", want: enum.OperationListInteractionRequests, call: func(ctx context.Context, s *Server) error {
			_, err := s.ListInteractionRequests(ctx, &interactionsv1.ListInteractionRequestsRequest{})
			return err
		}},
		{name: "RequestNotification", want: enum.OperationRequestNotification, call: func(ctx context.Context, s *Server) error {
			_, err := s.RequestNotification(ctx, &interactionsv1.RequestNotificationRequest{})
			return err
		}},
		{name: "UpsertSubscription", want: enum.OperationUpsertSubscription, call: func(ctx context.Context, s *Server) error {
			_, err := s.UpsertSubscription(ctx, &interactionsv1.UpsertSubscriptionRequest{})
			return err
		}},
		{name: "DisableSubscription", want: enum.OperationDisableSubscription, call: func(ctx context.Context, s *Server) error {
			_, err := s.DisableSubscription(ctx, &interactionsv1.DisableSubscriptionRequest{})
			return err
		}},
		{name: "ListSubscriptions", want: enum.OperationListSubscriptions, call: func(ctx context.Context, s *Server) error {
			_, err := s.ListSubscriptions(ctx, &interactionsv1.ListSubscriptionsRequest{})
			return err
		}},
		{name: "PlanDelivery", want: enum.OperationPlanDelivery, call: func(ctx context.Context, s *Server) error {
			_, err := s.PlanDelivery(ctx, &interactionsv1.PlanDeliveryRequest{})
			return err
		}},
		{name: "RecordDeliveryResult", want: enum.OperationRecordDeliveryResult, call: func(ctx context.Context, s *Server) error {
			_, err := s.RecordDeliveryResult(ctx, &interactionsv1.RecordDeliveryResultRequest{})
			return err
		}},
		{name: "RecordChannelCallback", want: enum.OperationRecordChannelCallback, call: func(ctx context.Context, s *Server) error {
			_, err := s.RecordChannelCallback(ctx, &interactionsv1.RecordChannelCallbackRequest{})
			return err
		}},
		{name: "GetDeliveryStatus", want: enum.OperationGetDeliveryStatus, call: func(ctx context.Context, s *Server) error {
			_, err := s.GetDeliveryStatus(ctx, &interactionsv1.GetDeliveryStatusRequest{})
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			service := &fakeInteractionService{}
			err := tc.call(context.Background(), NewServer(service))
			if err != nil {
				t.Fatalf("%s() err = %v", tc.name, err)
			}
			if len(service.operations) != 1 || service.operations[0] != tc.want {
				t.Fatalf("operations = %v, want %s", service.operations, tc.want)
			}
		})
	}
}

func TestUnaryErrorInterceptorMapsBacklogToUnimplemented(t *testing.T) {
	t.Parallel()

	interceptor := UnaryErrorInterceptor(slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := interceptor(context.Background(), nil, &grpcruntime.UnaryServerInfo{FullMethod: "/kodex.interactions.v1.InteractionHubService/RequestFeedback"}, func(context.Context, any) (any, error) {
		return nil, errs.ErrNotImplemented
	})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("code = %s, want Unimplemented", status.Code(err))
	}
}

type fakeInteractionService struct {
	operations []enum.Operation
	err        error
}

func (f *fakeInteractionService) record(operation enum.Operation) error {
	f.operations = append(f.operations, operation)
	if f.err != nil {
		return f.err
	}
	return nil
}

func (f *fakeInteractionService) CreateConversationThread(context.Context) error {
	return f.record(enum.OperationCreateConversationThread)
}

func (f *fakeInteractionService) RecordConversationMessage(context.Context) error {
	return f.record(enum.OperationRecordConversationMessage)
}

func (f *fakeInteractionService) GetConversationThread(context.Context) error {
	return f.record(enum.OperationGetConversationThread)
}

func (f *fakeInteractionService) ListConversationMessages(context.Context) error {
	return f.record(enum.OperationListConversationMessages)
}

func (f *fakeInteractionService) RequestFeedback(context.Context) error {
	return f.record(enum.OperationRequestFeedback)
}

func (f *fakeInteractionService) RequestApproval(context.Context) error {
	return f.record(enum.OperationRequestApproval)
}

func (f *fakeInteractionService) RequestHumanGate(context.Context) error {
	return f.record(enum.OperationRequestHumanGate)
}

func (f *fakeInteractionService) RecordInteractionResponse(context.Context) error {
	return f.record(enum.OperationRecordInteractionResponse)
}

func (f *fakeInteractionService) CancelInteractionRequest(context.Context) error {
	return f.record(enum.OperationCancelInteractionRequest)
}

func (f *fakeInteractionService) ExpireInteractionRequests(context.Context) error {
	return f.record(enum.OperationExpireInteractionRequests)
}

func (f *fakeInteractionService) GetInteractionRequest(context.Context) error {
	return f.record(enum.OperationGetInteractionRequest)
}

func (f *fakeInteractionService) ListInteractionRequests(context.Context) error {
	return f.record(enum.OperationListInteractionRequests)
}

func (f *fakeInteractionService) RequestNotification(context.Context) error {
	return f.record(enum.OperationRequestNotification)
}

func (f *fakeInteractionService) UpsertSubscription(context.Context) error {
	return f.record(enum.OperationUpsertSubscription)
}

func (f *fakeInteractionService) DisableSubscription(context.Context) error {
	return f.record(enum.OperationDisableSubscription)
}

func (f *fakeInteractionService) ListSubscriptions(context.Context) error {
	return f.record(enum.OperationListSubscriptions)
}

func (f *fakeInteractionService) PlanDelivery(context.Context) error {
	return f.record(enum.OperationPlanDelivery)
}

func (f *fakeInteractionService) RecordDeliveryResult(context.Context) error {
	return f.record(enum.OperationRecordDeliveryResult)
}

func (f *fakeInteractionService) RecordChannelCallback(context.Context) error {
	return f.record(enum.OperationRecordChannelCallback)
}

func (f *fakeInteractionService) GetDeliveryStatus(context.Context) error {
	return f.record(enum.OperationGetDeliveryStatus)
}

var _ interactionService = (*fakeInteractionService)(nil)

func TestServerReturnsDomainError(t *testing.T) {
	t.Parallel()

	service := &fakeInteractionService{err: errs.ErrNotImplemented}
	_, err := NewServer(service).RequestFeedback(context.Background(), &interactionsv1.RequestFeedbackRequest{})
	if !errors.Is(err, errs.ErrNotImplemented) {
		t.Fatalf("RequestFeedback() err = %v, want ErrNotImplemented", err)
	}
}
