package emailpolicy

import "testing"

func TestMailboxActionsMatchLifecycle(t *testing.T) {
	for _, test := range []struct {
		state           ActionState
		allowed, denied []string
	}{
		{ActionState{ManagedBy: "UI", RevisionState: "DRAFT", HasMutableDraft: true, ConnectionEnabled: true}, []string{"SAVE", "VALIDATE", "DISCARD"}, []string{"CREATE_DRAFT", "PUBLISH", "BIND", "UNBIND", "DETACH", "COPY"}},
		{ActionState{ManagedBy: "UI", RevisionState: "VALID", HasMutableDraft: true, ConnectionEnabled: true}, []string{"SAVE", "VALIDATE", "DISCARD", "PUBLISH"}, []string{"CREATE_DRAFT", "BIND"}},
		{ActionState{ManagedBy: "UI", RevisionState: "PUBLISHED", ConnectionEnabled: true, Bound: true}, []string{"CREATE_DRAFT", "BIND", "UNBIND"}, []string{"SAVE", "VALIDATE", "PUBLISH", "DISCARD"}},
		{ActionState{ManagedBy: "UI", RevisionState: "PUBLISHED", ConnectionEnabled: true, Bound: true, PendingDelivery: true}, []string{"CREATE_DRAFT"}, []string{"BIND", "UNBIND"}},
		{ActionState{ManagedBy: "GIT", RevisionState: "PUBLISHED", HasPublishedRevision: true, ConnectionEnabled: true, Bound: true}, []string{"DETACH", "COPY"}, []string{"CREATE_DRAFT", "SAVE", "VALIDATE", "PUBLISH", "DISCARD", "BIND", "UNBIND"}},
	} {
		actions := AvailableActions(test.state)
		for _, action := range test.allowed {
			if !ActionAllowed(actions, action) {
				t.Fatalf("required action %s denied", action)
			}
		}
		for _, action := range test.denied {
			if ActionAllowed(actions, action) {
				t.Fatalf("forbidden action %s allowed", action)
			}
		}
		for _, action := range actions {
			if action.Enabled != (action.Reason == "NONE") {
				t.Fatal("action reason contradicts allowance")
			}
		}
	}
}
