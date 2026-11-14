package grpc

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// castTelegramDeliveryPin preserves the exact private owner input across RPC without projecting its destination into the public subscription receipt.
// castTelegramDeliveryPin сохраняет точный private owner input через RPC, не проецируя его destination в публичную квитанцию подписки.
func castTelegramDeliveryPin(snapshot integrationpackage.TelegramDeliverySnapshot, digest string) *cp.TelegramDeliveryPin {
	configuration, _ := integrationpackage.ParseTelegramConfiguration(snapshot.Configuration)
	return &cp.TelegramDeliveryPin{SubscriptionRef: snapshot.SubscriptionRef, SubscriptionVersion: snapshot.SubscriptionVersion,
		ScopeDigest: snapshot.ScopeDigest, BotId: configuration.BotID, ChatId: configuration.ChatID, TopicId: configuration.TopicID,
		EventType: snapshot.EventType, TemplateKey: snapshot.TemplateKey, InputDigest: digest, ExpiresAt: timestamppb.New(snapshot.ExpiresAt),
		ProjectRef: snapshot.ProjectRef, PublicOrigin: snapshot.PublicOrigin, Configuration: snapshot.Configuration}
}

// castTelegramSubscription projects the safe owner receipt and deliberately has no private chat/token/descriptor fields to expose.
// castTelegramSubscription проецирует безопасный owner receipt и намеренно не имеет полей private chat/token/descriptor для выдачи.
func castTelegramSubscription(item entity.TelegramSubscription) *cp.TelegramSubscription {
	return &cp.TelegramSubscription{Ref: item.Ref, ProjectRef: item.ProjectRef, ConnectionRef: item.ConnectionRef, Version: item.Version, Enabled: item.Enabled, State: item.State,
		EventTypes: append([]string{}, item.EventTypes...), ScopeDigest: item.ScopeDigest, BotId: item.BotID, ChatDisplay: item.ChatDisplay, TopicDisplay: item.TopicDisplay,
		ExpiresAt: timestamppb.New(item.ExpiresAt), MaxEffects: item.MaxEffects, UsedEffects: item.UsedEffects, UpdatedAt: timestamppb.New(item.UpdatedAt), CanManage: item.CanManage}
}

// ConfigureTelegramSubscription binds the specialized owner command to the verified transport actor; project and connection values remain resource locators.
// ConfigureTelegramSubscription связывает специализированную owner команду с проверенным transport actor; значения project и connection остаются resource locators.
func (server *Server) ConfigureTelegramSubscription(ctx context.Context, request *cp.ConfigureTelegramSubscriptionRequest) (*cp.ConfigureTelegramSubscriptionResponse, error) {
	p, err := principal(ctx, cp.PlatformCommandService_ConfigureTelegramSubscription_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.ConfigureTelegramSubscription(ctx, p, mutation(request.GetMutation()), port.TelegramSubscriptionConfigure{ProjectRef: request.GetProjectRef(), ConnectionRef: request.GetConnectionRef(), Enabled: request.GetEnabled(), EventTypes: request.GetEventTypes(), DurationSeconds: request.GetDurationSeconds(), MaxEffects: request.GetMaxEffects()})
	if err != nil {
		return nil, transportError(err)
	}
	if result.Ref == "" || result.Version < 1 {
		return nil, transportError(errs.ErrUnavailable)
	}
	return &cp.ConfigureTelegramSubscriptionResponse{Subscription: castTelegramSubscription(result)}, nil
}

// GetTelegramSubscription reads one eligible project configuration and keeps absent subscription separate from an unauthorized project.
// GetTelegramSubscription читает одну доступную project configuration и отделяет отсутствие подписки от недоступного проекта.
func (server *Server) GetTelegramSubscription(ctx context.Context, request *cp.GetTelegramSubscriptionRequest) (*cp.GetTelegramSubscriptionResponse, error) {
	p, err := principal(ctx, cp.PlatformQueryService_GetTelegramSubscription_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, canManage, err := server.service.GetTelegramSubscription(ctx, p, request.GetProjectRef())
	if err != nil {
		return nil, transportError(err)
	}
	response := &cp.GetTelegramSubscriptionResponse{CanManage: canManage}
	if result != nil {
		response.Subscription = castTelegramSubscription(*result)
	}
	return response, nil
}

// ListInteractionDeliveries maps only safe eligible status rows with opaque paging; no private transport result is copied through Runtime maps.
// ListInteractionDeliveries преобразует лишь безопасные доступные status rows с opaque paging; private transport result через Runtime maps не копируется.
func (server *Server) ListInteractionDeliveries(ctx context.Context, request *cp.ListInteractionDeliveriesRequest) (*cp.ListInteractionDeliveriesResponse, error) {
	p, err := principal(ctx, cp.PlatformQueryService_ListInteractionDeliveries_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListInteractionDeliveries(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &cp.ListInteractionDeliveriesResponse{Items: []*cp.InteractionDeliveryView{}, Page: &cp.PageInfo{NextPageToken: next}}
	for _, item := range items {
		view := &cp.InteractionDeliveryView{Ref: item.Ref, Version: item.Version, ConnectionRef: item.ConnectionRef, RunRef: item.RunRef, GateRef: item.GateRef, EventType: item.EventType, State: item.State, Attempt: item.Attempt, Retryable: item.Retryable, SafeErrorCode: item.SafeErrorCode, CreatedAt: timestamppb.New(item.CreatedAt), AvailableAt: timestamppb.New(item.AvailableAt), ReceiptRef: item.ReceiptRef}
		if item.CompletedAt != nil {
			view.CompletedAt = timestamppb.New(*item.CompletedAt)
		}
		response.Items = append(response.Items, view)
	}
	return response, nil
}

// BeginInteractionDelivery carries only the exact leased input to the owner last-check; a replayed or denied response never becomes a new send permission.
// BeginInteractionDelivery передаёт лишь exact leased input последней owner проверке; replay либо denied response не становится новым send permission.
func (server *Server) BeginInteractionDelivery(ctx context.Context, request *cp.BeginInteractionDeliveryRequest) (*cp.BeginInteractionDeliveryResponse, error) {
	p, err := principal(ctx, cp.InteractionWorkService_BeginInteractionDelivery_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.BeginInteractionDelivery(ctx, p, port.InteractionDeliveryBeginInput{DeliveryRef: request.GetDeliveryRef(), LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(), InputDigest: request.GetInputDigest()})
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.BeginInteractionDeliveryResponse{DeliveryRef: result.DeliveryRef, Allowed: result.Allowed, AlreadyStarted: result.AlreadyStarted, InputDigest: result.InputDigest, SafeErrorCode: result.SafeErrorCode, RetryAfterSeconds: result.RetryAfterSeconds}, nil
}
