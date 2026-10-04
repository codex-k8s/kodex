package integration

import "testing"

func TestInvocationSelectedApprovalPolicyIsClosedBeforeEffects(t *testing.T) {
	t.Parallel()
	adapter := testAdapter(t)
	definition := adapter.definitions["github"]
	credential := testCredential(t, adapter, "policy-fixture-token")
	input := map[string]any{"pull_request_number": 7, "sha": "fixture-sha", "event": "COMMENT"}
	for _, policy := range []string{"NONE", "HUMAN_EACH_EFFECT", "HUMAN_SCOPED"} {
		request := invocationRequest(t, definition, "github.pull_request.review.create", input, credential)
		request.ApprovalPolicy = policy
		if _, _, _, _, err := adapter.validateInvocation(request); err != nil {
			t.Fatalf("approved selected policy %s was rejected: %v", policy, err)
		}
	}
	for _, mutate := range []func(*Request){
		func(r *Request) { r.ApprovalPolicy = "" },
		func(r *Request) { r.ApprovalPolicy = "UNKNOWN" },
		func(r *Request) { r.GrantRef = "" },
		func(r *Request) { r.GrantVersion = 0 },
		func(r *Request) { r.GrantVersion = -1 },
	} {
		request := invocationRequest(t, definition, "github.pull_request.review.create", input, credential)
		mutate(&request)
		if _, _, _, _, err := adapter.validateInvocation(request); err == nil {
			t.Fatal("invalid grant policy or pins passed invocation validation")
		}
	}
	for _, event := range []string{"APPROVE", "REQUEST_CHANGES"} {
		request := invocationRequest(t, definition, "github.pull_request.review.create", map[string]any{
			"pull_request_number": 7, "sha": "fixture-sha", "event": event,
		}, credential)
		request.ApprovalPolicy = "NONE"
		if _, _, _, _, err := adapter.validateInvocation(request); err == nil {
			t.Fatal("autonomous approval or request-changes passed invocation validation")
		}
	}
	request := invocationRequest(t, definition, "github.issue.create", map[string]any{"title": "fixture"}, credential)
	request.ApprovalPolicy = "NONE"
	if _, _, _, _, err := adapter.validateInvocation(request); err == nil {
		t.Fatal("unapproved autonomous write passed invocation validation")
	}
}
