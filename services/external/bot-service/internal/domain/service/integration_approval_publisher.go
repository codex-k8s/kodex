package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/integrations"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
)

// IntegrationApprovalPublisher публикует карточку только через защищённую human action boundary.
type IntegrationApprovalPublisher struct {
	localizer *texti18n.Localizer
	publisher MattermostThreadPublisher
	actionURL string
}

func NewIntegrationApprovalPublisher(localizer *texti18n.Localizer, publisher MattermostThreadPublisher, actionURL string) *IntegrationApprovalPublisher {
	return &IntegrationApprovalPublisher{localizer: localizer, publisher: publisher, actionURL: strings.TrimSpace(actionURL)}
}

func (publisher *IntegrationApprovalPublisher) EnsureApprovalCard(ctx context.Context, delivery integrations.ApprovalDelivery) (string, error) {
	if publisher == nil || publisher.publisher == nil || publisher.actionURL == "" {
		return "", fmt.Errorf("integration approval publisher is unavailable")
	}
	idempotent, ok := publisher.publisher.(MattermostIdempotentCardPublisher)
	if !ok {
		return "", fmt.Errorf("idempotent integration approval publisher is unavailable")
	}
	card := publisher.card(delivery)
	ref, err := idempotent.ReconcileOrPostThreadCard(ctx, card)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(ref.PostID) == "" || strings.TrimSpace(ref.ChannelID) != strings.TrimSpace(delivery.ChannelID) {
		return "", fmt.Errorf("integration approval card has no exact post binding")
	}
	return ref.PostID, nil
}

func (publisher *IntegrationApprovalPublisher) card(delivery integrations.ApprovalDelivery) MattermostCard {
	contextBase := map[string]any{
		"kind":                    "integration_approval",
		"resource_type":           "integration_approval",
		"resource_id":             delivery.ApprovalPublicID,
		"approval_binding_sha256": delivery.ApprovalBindingHash,
	}
	approveContext := cloneInteractionContext(contextBase)
	approveContext["action"] = string(integrations.ApprovalDecisionApprove)
	rejectContext := cloneInteractionContext(contextBase)
	rejectContext["action"] = string(integrations.ApprovalDecisionReject)
	return MattermostCard{
		ChannelID:  delivery.ChannelID,
		RootPostID: delivery.RootPostID,
		ActionURL:  publisher.actionURL,
		Message:    "matter-codex integration approval #notrigger",
		Props: map[string]any{
			"matter_codex_event":                   "integration_approval",
			"matter_codex_delivery_id":             delivery.ApprovalPublicID,
			"matter_codex_approval_id":             delivery.ApprovalPublicID,
			"matter_codex_invocation_id":           delivery.InvocationPublicID,
			"matter_codex_approval_binding_sha256": delivery.ApprovalBindingHash,
			"matter_codex_arguments_sha256":        delivery.ArgumentsHash,
		},
		Color: "#d99b00",
		Title: publisher.t("integration.approval.card.title", nil),
		Text: publisher.t("integration.approval.card.text", map[string]any{
			"Capability": delivery.CapabilityKey,
		}),
		Fields: []MattermostCardField{
			{Title: publisher.t("integration.approval.card.field.connection", nil), Value: delivery.ConnectionPublicID, Short: true},
			{Title: publisher.t("integration.approval.card.field.risk", nil), Value: delivery.RiskClass, Short: true},
			{Title: publisher.t("integration.approval.card.field.target", nil), Value: delivery.Arguments.Namespace + "/" + delivery.Arguments.WorkloadKind + "/" + delivery.Arguments.WorkloadName},
			{Title: publisher.t("integration.approval.card.field.expires", nil), Value: delivery.ExpiresAt.UTC().Format(time.RFC3339), Short: true},
		},
		Actions: []MattermostCardAction{
			{ID: "approveintegration", Name: publisher.t("integration.approval.action.approve", nil), Tooltip: publisher.t("integration.approval.action.approve.tooltip", nil), Style: "primary", Context: approveContext},
			{ID: "rejectintegration", Name: publisher.t("integration.approval.action.reject", nil), Tooltip: publisher.t("integration.approval.action.reject.tooltip", nil), Style: "danger", Context: rejectContext},
		},
		Interaction: MattermostCardInteraction{
			Actor: AuthenticatedActor{UserID: delivery.ApproverUserID, UserName: delivery.ApproverUserName},
			Scope: InteractionScope{Workspace: delivery.WorkspaceScope, Session: delivery.SessionScope},
		},
	}
}

func (publisher *IntegrationApprovalPublisher) t(messageID string, data map[string]any) string {
	if publisher.localizer == nil {
		return messageID
	}
	return publisher.localizer.T(messageID, data)
}

var _ integrations.ApprovalCardPublisher = (*IntegrationApprovalPublisher)(nil)
