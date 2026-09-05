package emailpolicy

import "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"

// ActionState поступает только после owner/permission проверки существующего connection.
type ActionState struct {
	ManagedBy, RevisionState                                                         string
	HasMutableDraft, HasPublishedRevision, Bound, PendingDelivery, ConnectionEnabled bool
}

func AvailableActions(state ActionState) []entity.EmailMailboxActionAvailability {
	result := make([]entity.EmailMailboxActionAvailability, 0, 9)
	mutable := state.RevisionState == "DRAFT" || state.RevisionState == "VALID" || state.RevisionState == "INVALID"
	for _, action := range []string{"CREATE_DRAFT", "SAVE", "VALIDATE", "PUBLISH", "DISCARD", "BIND", "UNBIND", "DETACH", "COPY"} {
		reason := "NONE"
		if action == "DETACH" || action == "COPY" {
			if state.ManagedBy != "GIT" || !state.HasPublishedRevision {
				reason = "STATE"
			}
		} else if state.ManagedBy == "GIT" {
			reason = "GIT_MANAGED"
		} else {
			switch action {
			case "CREATE_DRAFT":
				if state.HasMutableDraft {
					reason = "STATE"
				}
			case "SAVE", "VALIDATE", "DISCARD":
				if !mutable {
					reason = "STATE"
				}
			case "PUBLISH":
				if state.RevisionState != "VALID" {
					reason = "STATE"
				}
			case "BIND":
				if state.RevisionState != "PUBLISHED" {
					reason = "STATE"
				} else if !state.ConnectionEnabled {
					reason = "CONNECTION_DISABLED"
				} else if state.PendingDelivery {
					reason = "DELIVERY_PENDING"
				}
			case "UNBIND":
				if !state.Bound {
					reason = "NO_BINDING"
				} else if state.PendingDelivery {
					reason = "DELIVERY_PENDING"
				}
			}
		}
		result = append(result, entity.EmailMailboxActionAvailability{Action: action, Reason: reason, Enabled: reason == "NONE"})
	}
	return result
}

func ActionAllowed(actions []entity.EmailMailboxActionAvailability, action string) bool {
	for _, candidate := range actions {
		if candidate.Action == action {
			return candidate.Enabled
		}
	}
	return false
}
