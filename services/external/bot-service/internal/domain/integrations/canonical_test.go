package integrations

import (
	"errors"
	"testing"
)

func TestApprovalBindingHashBindsEveryAuthorityDimension(t *testing.T) {
	session := SessionContext{
		SessionKey: "session-one", SubjectKind: SubjectKindAgentRole, SubjectRef: "17",
		InstallationScope: InstallationScope, WorkspaceScope: "9",
	}
	binding := Binding{
		CapabilityKey: CapabilityRestartWorkload, CapabilityVersion: CapabilityVersion, CapabilityRevision: 3,
		ConnectionPublicID: "recording-main", ConnectionRevision: 4,
		GrantPublicID: "grant-main", GrantRevision: 5,
	}
	baseline, err := ApprovalBindingHash(session, binding, "inv_0123456789abcdef0123456789abcdef", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("ApprovalBindingHash(): %v", err)
	}
	mutations := []struct {
		name   string
		mutate func(*SessionContext, *Binding, *string, *string)
	}{
		{"invocation", func(_ *SessionContext, _ *Binding, invocation *string, _ *string) {
			*invocation = "inv_1123456789abcdef0123456789abcdef"
		}},
		{"capability revision", func(_ *SessionContext, item *Binding, _ *string, _ *string) { item.CapabilityRevision++ }},
		{"connection", func(_ *SessionContext, item *Binding, _ *string, _ *string) {
			item.ConnectionPublicID = "recording-other"
		}},
		{"connection revision", func(_ *SessionContext, item *Binding, _ *string, _ *string) { item.ConnectionRevision++ }},
		{"grant", func(_ *SessionContext, item *Binding, _ *string, _ *string) { item.GrantPublicID = "grant-other" }},
		{"grant revision", func(_ *SessionContext, item *Binding, _ *string, _ *string) { item.GrantRevision++ }},
		{"subject", func(item *SessionContext, _ *Binding, _ *string, _ *string) { item.SubjectRef = "18" }},
		{"workspace", func(item *SessionContext, _ *Binding, _ *string, _ *string) { item.WorkspaceScope = "10" }},
		{"session", func(item *SessionContext, _ *Binding, _ *string, _ *string) { item.SessionKey = "session-two" }},
		{"arguments", func(_ *SessionContext, _ *Binding, _ *string, arguments *string) {
			*arguments = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		}},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			mutatedSession, mutatedBinding := session, binding
			invocation := "inv_0123456789abcdef0123456789abcdef"
			arguments := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			test.mutate(&mutatedSession, &mutatedBinding, &invocation, &arguments)
			actual, err := ApprovalBindingHash(mutatedSession, mutatedBinding, invocation, arguments)
			if err != nil {
				t.Fatalf("ApprovalBindingHash(): %v", err)
			}
			if actual == baseline {
				t.Fatal("изменённое полномочие не изменило approval binding hash")
			}
		})
	}
}

func TestValidateRestartInputIsTypedAndFailClosed(t *testing.T) {
	valid := RestartWorkloadInput{
		Connection: "recording-main", Namespace: "mattermost", WorkloadKind: "Deployment",
		WorkloadName: "bot-service", IdempotencyKey: "restart:test:0001",
	}
	if _, err := validateRestartInput(valid); err != nil {
		t.Fatalf("valid input: %v", err)
	}
	invalid := []RestartWorkloadInput{
		{Connection: "recording-main", Namespace: "mattermost", WorkloadKind: "StatefulSet", WorkloadName: "bot-service", IdempotencyKey: "restart:test:0001"},
		{Connection: "recording-main", Namespace: "../mattermost", WorkloadKind: "Deployment", WorkloadName: "bot-service", IdempotencyKey: "restart:test:0001"},
		{Connection: "recording-main", Namespace: "mattermost", WorkloadKind: "Deployment", WorkloadName: "bot service", IdempotencyKey: "restart:test:0001"},
		{Connection: "recording-main", Namespace: "mattermost", WorkloadKind: "Deployment", WorkloadName: "bot-service", IdempotencyKey: "short"},
	}
	for _, input := range invalid {
		if _, err := validateRestartInput(input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid input error = %v", err)
		}
	}
}
