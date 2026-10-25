package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ReportEmailEffectReceipt validates the invocation binding before executing the effect report
// with external receipt and semantic input digests, requiring a receipt result.
// ReportEmailEffectReceipt проверяет привязку вызова до команды отчёта об эффекте с digest внешней
// квитанции и семантического ввода, требуя квитанцию в результате.
func (server *Server) ReportEmailEffectReceipt(ctx context.Context, request *controlplanev1.ReportEmailEffectReceiptRequest) (*controlplanev1.ReportEmailEffectReceiptResponse, error) {
	binding, err := emailBindingFromProto(request.GetBinding())
	if err != nil {
		return nil, transportError(err)
	}
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_ReportEmailEffectReceipt_FullMethodName,
		command.ReportEmailEffect, request.GetMutation(), command.EmailEffectReportInput{Binding: binding,
			ExternalReceiptRef: request.GetExternalReceiptRef(), ExternalReceiptDigest: request.GetExternalReceiptDigest(),
			SemanticInputDigest: request.GetSemanticInputDigest(), Outcome: emailOutcomeFromProto(request.GetOutcome()),
		})
	if err != nil {
		return nil, err
	}
	if result.EmailReceipt == nil {
		return nil, transportError(errs.ErrUnavailable)
	}
	return &controlplanev1.ReportEmailEffectReceiptResponse{Receipt: castEmailReceipt(*result.EmailReceipt)}, nil
}

// GetEmailEffectReceipt reads the invocation receipt through the verified query principal and
// projects its optional reconciliation decision.
// GetEmailEffectReceipt читает квитанцию вызова с проверенным query principal и преобразует её
// необязательное решение сверки.
func (server *Server) GetEmailEffectReceipt(ctx context.Context, request *controlplanev1.GetEmailEffectReceiptRequest) (*controlplanev1.GetEmailEffectReceiptResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_GetEmailEffectReceipt_FullMethodName, func(p value.Principal) (*controlplanev1.GetEmailEffectReceiptResponse, error) {
		view, err := server.service.GetEmailEffectReceipt(ctx, p, request.GetInvocationRef())
		if err != nil {
			return nil, err
		}
		return &controlplanev1.GetEmailEffectReceiptResponse{Receipt: castEmailReceipt(view.Receipt), Decision: castEmailDecision(view.Decision)}, nil

	})
}

// ReconcileEmailEffect passes receipt reference, expected digest, reported outcome and note
// through the dedicated reconciliation command and requires a decision.
// ReconcileEmailEffect передаёт ссылку квитанции, ожидаемый digest, сообщённый исход и примечание
// отдельной команде сверки и требует решение.
func (server *Server) ReconcileEmailEffect(ctx context.Context, request *controlplanev1.ReconcileEmailEffectRequest) (*controlplanev1.ReconcileEmailEffectResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ReconcileEmailEffect_FullMethodName,
		command.ReconcileEmailEffect, request.GetMutation(), command.EmailReconciliationInput{
			ReceiptRef: request.GetReceiptRef(), ExpectedReceiptDigest: request.GetExpectedReceiptDigest(),
			Outcome: emailOutcomeFromProto(request.GetOutcome()), Note: request.GetNote(),
		})
	if err != nil {
		return nil, err
	}
	if result.EmailDecision == nil {
		return nil, transportError(errs.ErrUnavailable)
	}
	return &controlplanev1.ReconcileEmailEffectResponse{Decision: castEmailDecision(result.EmailDecision)}, nil
}

// ResolveEmailReconciliation passes exact receipt, decision and external receipt locators to the
// authorized worker resolution and projects the returned view.
// ResolveEmailReconciliation передаёт точные ссылки квитанции, решения и внешней квитанции
// авторизованному разрешению worker и преобразует результат.
func (server *Server) ResolveEmailReconciliation(ctx context.Context, request *controlplanev1.ResolveEmailReconciliationRequest) (*controlplanev1.ResolveEmailReconciliationResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeWorkService_ResolveEmailReconciliation_FullMethodName)
	if err != nil {
		return nil, err
	}
	view, err := server.service.ResolveEmailReconciliation(ctx, p, request.GetReceiptRef(), request.GetDecisionRef(), request.GetExternalReceiptRef(), request.GetExternalReceiptDigest())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.ResolveEmailReconciliationResponse{Receipt: castEmailReceipt(view.Receipt), Decision: castEmailDecision(view.Decision)}, nil
}

// emailOutcomeFromProto maps the three supported wire effect outcomes to domain constants and
// returns an empty string for all other values.
// emailOutcomeFromProto переводит три поддерживаемых исхода эффекта протокола в доменные константы
// и возвращает пустую строку для прочих значений.
func emailOutcomeFromProto(input controlplanev1.EmailEffectOutcome) string {
	switch input {
	case controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_UNKNOWN_OUTCOME:
		return emailpolicy.OutcomeUnknown
	case controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_EFFECT_CONFIRMED:
		return emailpolicy.OutcomeEffectConfirmed
	case controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_NO_EFFECT_CONFIRMED:
		return emailpolicy.OutcomeNoEffectConfirmed
	default:
		return ""
	}
}

// emailOutcomeProto maps the three domain effect outcomes to protobuf and uses UNSPECIFIED for
// every unknown string.
// emailOutcomeProto переводит три доменных исхода эффекта в Protobuf и использует UNSPECIFIED для
// неизвестной строки.
func emailOutcomeProto(input string) controlplanev1.EmailEffectOutcome {
	switch input {
	case emailpolicy.OutcomeUnknown:
		return controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_UNKNOWN_OUTCOME
	case emailpolicy.OutcomeEffectConfirmed:
		return controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_EFFECT_CONFIRMED
	case emailpolicy.OutcomeNoEffectConfirmed:
		return controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_NO_EFFECT_CONFIRMED
	default:
		return controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_UNSPECIFIED
	}
}

// castEmailReceipt projects persisted receipt identity, version, invocation, effect and
// configuration pins with outcome and timestamps.
// castEmailReceipt преобразует сохранённые identity, версию и привязки вызова, эффекта и
// конфигурации квитанции вместе с исходом и временем.
func castEmailReceipt(r entity.EmailEffectReceipt) *controlplanev1.EmailEffectReceipt {
	return &controlplanev1.EmailEffectReceipt{Ref: r.Ref, Version: r.Version, InvocationRef: r.InvocationRef,
		ExternalReceiptRef: r.ExternalReceiptRef, ExternalReceiptDigest: r.ExternalReceiptDigest,
		SemanticInputDigest: r.SemanticInputDigest, EffectKey: r.EffectKey, Outcome: emailOutcomeProto(r.Outcome),
		MailboxRef: r.MailboxRef, ConfigurationRevision: r.ConfigurationRevision, ConnectionRef: r.ConnectionRef,
		ProjectRef: r.ProjectRef, CreatedAt: timestamppb.New(r.CreatedAt), UpdatedAt: timestamppb.New(r.UpdatedAt)}
}

// castEmailDecision preserves an absent decision and projects receipt/version/digest pins,
// invocation, outcome, grant, actor and creation/expiry timestamps.
// castEmailDecision сохраняет отсутствие решения и преобразует привязки квитанции, версии и
// digest, вызов, исход, grant, actor и время создания и истечения.
func castEmailDecision(d *entity.EmailReconciliationDecision) *controlplanev1.EmailReconciliationDecision {
	if d == nil {
		return nil
	}
	return &controlplanev1.EmailReconciliationDecision{Ref: d.Ref, Version: d.Version, ReceiptRef: d.ReceiptRef,
		ReceiptVersion: d.ReceiptVersion, ReceiptDigest: d.ReceiptDigest, InvocationRef: d.InvocationRef,
		Outcome: emailOutcomeProto(d.Outcome), GrantRef: d.GrantRef, ActorRef: d.ActorRef,
		CreatedAt: timestamppb.New(d.CreatedAt), ExpiresAt: timestamppb.New(d.ExpiresAt)}
}
