package casters

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/value"
)

func TestResponseActionCastsRequestChanges(t *testing.T) {
	t.Parallel()

	if got := ResponseAction(interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_REQUEST_CHANGES); got != enum.InteractionResponseActionRequestChanges {
		t.Fatalf("ResponseAction() = %s, want request_changes", got)
	}
	if got := ResponseActionProto(enum.InteractionResponseActionRequestChanges); got != interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_REQUEST_CHANGES {
		t.Fatalf("ResponseActionProto() = %s, want request_changes", got)
	}
}

func TestOwnerInboxItemCastsAllowedActionsWithoutRawCallbackPayload(t *testing.T) {
	t.Parallel()

	rawCallbackPayload := "raw callback payload with token-like content"
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	requestID := uuid.New()
	dto := OwnerInboxItem(entity.OwnerInboxItem{
		Request: entity.InteractionRequest{
			ID:          requestID,
			RequestKind: enum.InteractionRequestKindHumanGate,
			Scope:       value.ScopeRef{Type: enum.ScopeTypeService, Ref: "agent-manager"},
			SourceOwner: value.SourceOwnerRef{Kind: enum.SourceOwnerKindAgentManager, Ref: "run:123"},
			TargetRefs:  []value.ActorRef{{Kind: "user", Ref: "owner-1"}},
			AllowedActions: []value.InteractionAction{
				{ActionKey: "approve", LabelTemplateRef: "interaction.actions.approve", Terminal: true},
				{ActionKey: "reject", LabelTemplateRef: "interaction.actions.reject", Terminal: true},
				{ActionKey: "request_changes", LabelTemplateRef: "interaction.actions.request_changes", Terminal: true},
			},
			Status:    enum.InteractionRequestStatusWaiting,
			CreatedAt: now,
			UpdatedAt: now,
			Version:   1,
		},
		Title:   "safe title",
		Summary: "safe summary",
		LatestCallback: &entity.ChannelCallback{
			ID:                  uuid.New(),
			CallbackID:          "callback-1",
			RequestID:           &requestID,
			ActorRef:            "user:owner-1",
			Action:              "approve",
			CallbackSummary:     rawCallbackPayload,
			CallbackFingerprint: "sha256:callback",
			SignatureStatus:     enum.CallbackSignatureStatusVerified,
			ProcessingStatus:    enum.CallbackProcessingStatusAccepted,
			ReceivedAt:          now,
			CreatedAt:           now,
		},
	})

	if len(dto.GetAllowedActions()) != 3 || dto.GetAllowedActions()[2].GetActionKey() != "request_changes" {
		t.Fatalf("allowed_actions = %+v, want owner-safe actions", dto.GetAllowedActions())
	}
	if strings.Contains(dto.String(), rawCallbackPayload) {
		t.Fatalf("owner inbox dto leaked raw callback payload: %s", dto.String())
	}
	if dto.GetLatestCallback().GetAction() != "approve" || dto.GetLatestCallback().GetCallbackId() != "callback-1" {
		t.Fatalf("latest callback = %+v, want safe callback refs", dto.GetLatestCallback())
	}
}

func TestOwnerInboxItemOmitsAllowedActionsForTerminalRequests(t *testing.T) {
	t.Parallel()

	for _, status := range []enum.InteractionRequestStatus{
		enum.InteractionRequestStatusAnswered,
		enum.InteractionRequestStatusCancelled,
		enum.InteractionRequestStatusExpired,
		enum.InteractionRequestStatusFailed,
	} {
		status := status
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()

			dto := OwnerInboxItem(entity.OwnerInboxItem{
				Request: entity.InteractionRequest{
					ID:          uuid.New(),
					RequestKind: enum.InteractionRequestKindHumanGate,
					Scope:       value.ScopeRef{Type: enum.ScopeTypeService, Ref: "agent-manager"},
					SourceOwner: value.SourceOwnerRef{Kind: enum.SourceOwnerKindAgentManager, Ref: "run:123"},
					AllowedActions: []value.InteractionAction{
						{ActionKey: "approve", LabelTemplateRef: "interaction.actions.approve", Terminal: true},
					},
					Status:    status,
					CreatedAt: time.Date(2026, 5, 27, 12, 30, 0, 0, time.UTC),
					UpdatedAt: time.Date(2026, 5, 27, 12, 30, 0, 0, time.UTC),
					Version:   2,
				},
			})
			if len(dto.GetAllowedActions()) != 0 {
				t.Fatalf("allowed_actions for %s = %+v, want none", status, dto.GetAllowedActions())
			}
		})
	}
}
