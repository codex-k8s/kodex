package controlplaneapi

import (
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAgentMattermostBotIntentProducerConsumerCanonicalEquality(t *testing.T) {
	t.Parallel()
	authority := testAuthority(controlplanev1.ControlPlaneService_ManageAgentMattermostBotIdentity_FullMethodName)
	request := &controlplanev1.ManageAgentMattermostBotIdentityRequest{
		Action:  controlplanev1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_REBIND,
		AgentId: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", ExpectedVersion: 7,
	}
	producer, err := AgentMattermostBotIdentityIntentSHA256(authority, request, "agent-primary")
	if err != nil {
		t.Fatal(err)
	}
	consumer := proto.Clone(request).(*controlplanev1.ManageAgentMattermostBotIdentityRequest)
	consumer.IdempotencyKey = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	consumer.ProviderReceipt = &controlplanev1.ProviderEffectReadbackReceipt{
		ReceiptId: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", ReceiptRevision: 8,
		CommandIntentSha256: producer,
	}
	actual, err := AgentMattermostBotIdentityIntentSHA256(authority, consumer, "agent-primary")
	if err != nil || actual != producer {
		t.Fatalf("Agent bot producer/consumer canonical mismatch: %q %q %v", producer, actual, err)
	}
}

func TestAgentMattermostBotIntentBindsAuthorityTargetActionAndVersion(t *testing.T) {
	t.Parallel()
	authority := testAuthority(controlplanev1.ControlPlaneService_ManageAgentMattermostBotIdentity_FullMethodName)
	request := &controlplanev1.ManageAgentMattermostBotIdentityRequest{
		Action:  controlplanev1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_BIND,
		AgentId: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", ExpectedVersion: 7,
	}
	base, err := AgentMattermostBotIdentityIntentSHA256(authority, request, "agent-primary")
	if err != nil {
		t.Fatal(err)
	}
	assertChanged := func(name string, changedAuthority VerifiedCommandAuthority,
		changedRequest *controlplanev1.ManageAgentMattermostBotIdentityRequest, stableKey string,
	) {
		t.Helper()
		digest, digestErr := AgentMattermostBotIdentityIntentSHA256(changedAuthority, changedRequest, stableKey)
		if digestErr != nil || digest == base {
			t.Fatalf("Agent bot intent field is not bound (%s): %q %v", name, digest, digestErr)
		}
	}
	changedAuthority := authority
	changedAuthority.ActorID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	assertChanged("actor", changedAuthority, request, "agent-primary")
	changed := proto.Clone(request).(*controlplanev1.ManageAgentMattermostBotIdentityRequest)
	changed.Action = controlplanev1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_REVOKE
	assertChanged("action", authority, changed, "agent-primary")
	changed = proto.Clone(request).(*controlplanev1.ManageAgentMattermostBotIdentityRequest)
	changed.ExpectedVersion++
	assertChanged("version", authority, changed, "agent-primary")
	changed = proto.Clone(request).(*controlplanev1.ManageAgentMattermostBotIdentityRequest)
	changed.AgentId = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	assertChanged("target", authority, changed, "agent-primary")
	assertChanged("stable-key", authority, request, "agent-secondary")
}

func TestProviderIntentProducerConsumerCanonicalEquality(t *testing.T) {
	t.Parallel()

	authority := testAuthority(controlplanev1.ControlPlaneService_ManageProviderConnectionReference_FullMethodName)
	request := testProviderRequest()
	producer, err := ProviderConnectionReferenceIntentSHA256(authority, request)
	if err != nil {
		t.Fatal(err)
	}
	consumerRequest := proto.Clone(request).(*controlplanev1.ManageProviderConnectionReferenceRequest)
	consumerRequest.IdempotencyKey = "different-transport-key"
	consumerRequest.ProviderReceipt = &controlplanev1.ProviderEffectReadbackReceipt{
		ReceiptId: "99999999-9999-4999-8999-999999999999", ReceiptRevision: 99,
		CommandIntentSha256: producer,
	}
	consumer, err := ProviderConnectionReferenceIntentSHA256(authority, consumerRequest)
	if err != nil || producer != consumer {
		t.Fatalf("producer/consumer canonical intent mismatch: %q %q %v", producer, consumer, err)
	}
}

func TestProviderIntentBindsEveryStableAuthorityAndBusinessField(t *testing.T) {
	t.Parallel()

	authority := testAuthority(controlplanev1.ControlPlaneService_ManageProviderConnectionReference_FullMethodName)
	request := testProviderRequest()
	base, err := ProviderConnectionReferenceIntentSHA256(authority, request)
	if err != nil {
		t.Fatal(err)
	}
	authorityMutations := []struct {
		name   string
		mutate func(*VerifiedCommandAuthority)
	}{
		{"actor", func(value *VerifiedCommandAuthority) { value.ActorID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" }},
		{"organization", func(value *VerifiedCommandAuthority) { value.OrganizationID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb" }},
		{"project", func(value *VerifiedCommandAuthority) { value.ProjectID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc" }},
		{"workload", func(value *VerifiedCommandAuthority) { value.WorkloadID = "integration-gateway-canary" }},
	}
	for _, item := range authorityMutations {
		t.Run(item.name, func(t *testing.T) {
			changed := authority
			item.mutate(&changed)
			assertProviderIntentChanged(t, base, changed, request)
		})
	}
	requestMutations := []struct {
		name   string
		mutate func(*controlplanev1.ManageProviderConnectionReferenceRequest)
	}{
		{"action", func(value *controlplanev1.ManageProviderConnectionReferenceRequest) {
			value.Action = controlplanev1.ProviderConnectionReferenceAction_PROVIDER_CONNECTION_REFERENCE_ACTION_REFRESH
		}},
		{"target", func(value *controlplanev1.ManageProviderConnectionReferenceRequest) {
			value.ProviderConnectionReferenceId = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
		}},
		{"version", func(value *controlplanev1.ManageProviderConnectionReferenceRequest) { value.ExpectedVersion++ }},
		{"name", func(value *controlplanev1.ManageProviderConnectionReferenceRequest) { value.Name += " changed" }},
		{"stable-key", func(value *controlplanev1.ManageProviderConnectionReferenceRequest) {
			value.Spec.StableKey = "provider-secondary"
		}},
		{"provider", func(value *controlplanev1.ManageProviderConnectionReferenceRequest) {
			value.Spec.Provider = "provider-secondary"
		}},
		{"server-reference", func(value *controlplanev1.ManageProviderConnectionReferenceRequest) {
			value.Spec.ServerReference = "connection-secondary"
		}},
		{"reference-version", func(value *controlplanev1.ManageProviderConnectionReferenceRequest) { value.Spec.ReferenceVersion++ }},
		{"reference-generation", func(value *controlplanev1.ManageProviderConnectionReferenceRequest) { value.Spec.ReferenceGeneration++ }},
		{"reference-digest", func(value *controlplanev1.ManageProviderConnectionReferenceRequest) {
			value.Spec.ReferenceSha256 = repeatHex("b")
		}},
		{"masked-label", func(value *controlplanev1.ManageProviderConnectionReferenceRequest) {
			value.Spec.MaskedLabel = "secondary"
		}},
		{"masked-status", func(value *controlplanev1.ManageProviderConnectionReferenceRequest) {
			value.Spec.MaskedStatus = controlplanev1.ProviderConnectionStatus_PROVIDER_CONNECTION_STATUS_DEGRADED
		}},
		{"capabilities", func(value *controlplanev1.ManageProviderConnectionReferenceRequest) {
			value.Spec.Capabilities = append(value.Spec.Capabilities, "provider-health")
		}},
		{"eligible", func(value *controlplanev1.ManageProviderConnectionReferenceRequest) { value.Spec.Eligible = false }},
		{"observed-at", func(value *controlplanev1.ManageProviderConnectionReferenceRequest) {
			value.Spec.ObservedAt = timestamppb.New(value.Spec.ObservedAt.AsTime().Add(time.Second))
		}},
		{"credential-id", func(value *controlplanev1.ManageProviderConnectionReferenceRequest) {
			value.Spec.CredentialBindingId = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
		}},
		{"credential-version", func(value *controlplanev1.ManageProviderConnectionReferenceRequest) {
			value.Spec.CredentialBindingVersion++
		}},
		{"credential-digest", func(value *controlplanev1.ManageProviderConnectionReferenceRequest) {
			value.Spec.CredentialBindingSha256 = repeatHex("c")
		}},
	}
	for _, item := range requestMutations {
		t.Run(item.name, func(t *testing.T) {
			changed := proto.Clone(request).(*controlplanev1.ManageProviderConnectionReferenceRequest)
			item.mutate(changed)
			assertProviderIntentChanged(t, base, authority, changed)
		})
	}
	wrongMethod := authority
	wrongMethod.FullMethod = controlplanev1.ControlPlaneService_ReconcileGitAgent_FullMethodName
	if _, err := ProviderConnectionReferenceIntentSHA256(wrongMethod, request); err == nil {
		t.Fatal("changed exact full method was accepted")
	}
}

func TestProviderPoolIntentProducerConsumerCanonicalEquality(t *testing.T) {
	t.Parallel()
	authority := testAuthority(controlplanev1.ControlPlaneService_ManageProviderPool_FullMethodName)
	request := testProviderPoolRequest()
	producer, err := ProviderPoolIntentSHA256(authority, request)
	if err != nil {
		t.Fatal(err)
	}
	consumer := proto.Clone(request).(*controlplanev1.ManageProviderPoolRequest)
	consumer.IdempotencyKey = "another-transport-key"
	consumer.ProviderReceipt = &controlplanev1.ProviderEffectReadbackReceipt{ReceiptId: "88888888-8888-4888-8888-888888888888", CommandIntentSha256: producer}
	actual, err := ProviderPoolIntentSHA256(authority, consumer)
	if err != nil || actual != producer {
		t.Fatalf("provider pool producer/consumer canonical mismatch: %q %q %v", producer, actual, err)
	}
}

func TestProviderPoolIntentBindsAuthorityAndCapacityBusinessFields(t *testing.T) {
	t.Parallel()
	authority := testAuthority(controlplanev1.ControlPlaneService_ManageProviderPool_FullMethodName)
	request := testProviderPoolRequest()
	base, err := ProviderPoolIntentSHA256(authority, request)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*VerifiedCommandAuthority){
		"actor":        func(value *VerifiedCommandAuthority) { value.ActorID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" },
		"organization": func(value *VerifiedCommandAuthority) { value.OrganizationID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb" },
		"project":      func(value *VerifiedCommandAuthority) { value.ProjectID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc" },
		"workload":     func(value *VerifiedCommandAuthority) { value.WorkloadID = "integration-gateway-canary" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := authority
			mutate(&changed)
			digest, digestErr := ProviderPoolIntentSHA256(changed, request)
			if digestErr != nil || digest == base {
				t.Fatalf("authority field is not bound: %q %v", digest, digestErr)
			}
		})
	}
	mutations := map[string]func(*controlplanev1.ManageProviderPoolRequest){
		"action": func(value *controlplanev1.ManageProviderPoolRequest) {
			value.Action = controlplanev1.ProviderPoolAction_PROVIDER_POOL_ACTION_ARCHIVE
		},
		"target": func(value *controlplanev1.ManageProviderPoolRequest) {
			value.ProviderPoolId = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		},
		"version":         func(value *controlplanev1.ManageProviderPoolRequest) { value.ExpectedVersion++ },
		"name":            func(value *controlplanev1.ManageProviderPoolRequest) { value.Name += " changed" },
		"stable-key":      func(value *controlplanev1.ManageProviderPoolRequest) { value.Spec.StableKey += "-changed" },
		"policy":          func(value *controlplanev1.ManageProviderPoolRequest) { value.Spec.Policy = "WEIGHTED" },
		"policy-revision": func(value *controlplanev1.ManageProviderPoolRequest) { value.Spec.PolicyRevision++ },
		"eligibility-digest": func(value *controlplanev1.ManageProviderPoolRequest) {
			value.Spec.EligibilitySnapshotSha256 = repeatHex("b")
		},
		"connection-ref": func(value *controlplanev1.ManageProviderPoolRequest) {
			value.Spec.Bindings[0].ProviderConnectionReferenceId = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		},
		"connection-version": func(value *controlplanev1.ManageProviderPoolRequest) { value.Spec.Bindings[0].ReferenceVersion++ },
		"connection-digest": func(value *controlplanev1.ManageProviderPoolRequest) {
			value.Spec.Bindings[0].ReferenceSha256 = repeatHex("c")
		},
		"weight":               func(value *controlplanev1.ManageProviderPoolRequest) { value.Spec.Bindings[0].Weight++ },
		"eligible":             func(value *controlplanev1.ManageProviderPoolRequest) { value.Spec.Bindings[0].Eligible = false },
		"usage":                func(value *controlplanev1.ManageProviderPoolRequest) { value.Spec.Bindings[0].ObservedUsage++ },
		"limit":                func(value *controlplanev1.ManageProviderPoolRequest) { value.Spec.Bindings[0].ObservedLimit++ },
		"observation-revision": func(value *controlplanev1.ManageProviderPoolRequest) { value.Spec.Bindings[0].ObservationRevision++ },
		"observation-time": func(value *controlplanev1.ManageProviderPoolRequest) {
			value.Spec.Bindings[0].ObservedAt = timestamppb.New(value.Spec.Bindings[0].ObservedAt.AsTime().Add(time.Second))
		},
		"observation-expiry": func(value *controlplanev1.ManageProviderPoolRequest) {
			value.Spec.Bindings[0].ObservationExpiresAt = timestamppb.New(value.Spec.Bindings[0].ObservationExpiresAt.AsTime().Add(time.Second))
		},
		"observation-digest": func(value *controlplanev1.ManageProviderPoolRequest) {
			value.Spec.Bindings[0].ObservationSha256 = repeatHex("d")
		},
		"window-duration": func(value *controlplanev1.ManageProviderPoolRequest) {
			value.Spec.Bindings[0].WindowDurationSeconds++
		},
		"reset-time": func(value *controlplanev1.ManageProviderPoolRequest) {
			value.Spec.Bindings[0].ResetsAt = timestamppb.New(value.Spec.Bindings[0].ResetsAt.AsTime().Add(time.Second))
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			changed := proto.Clone(request).(*controlplanev1.ManageProviderPoolRequest)
			mutate(changed)
			digest, digestErr := ProviderPoolIntentSHA256(authority, changed)
			if digestErr != nil || digest == base {
				t.Fatalf("provider pool business field is not bound: %q %v", digest, digestErr)
			}
		})
	}
	wrongMethod := authority
	wrongMethod.FullMethod = controlplanev1.ControlPlaneService_ManageProviderConnectionReference_FullMethodName
	if _, err = ProviderPoolIntentSHA256(wrongMethod, request); err == nil {
		t.Fatal("changed provider pool full method was accepted")
	}
}

func TestProviderCredentialMaterializationBindsGenerationAndObservation(t *testing.T) {
	t.Parallel()
	value := ProviderCredentialMaterialization{
		CredentialBindingID: "44444444-4444-4444-8444-444444444444", BindingVersion: 1, CredentialGeneration: 3,
		Provider: "openai-codex", ProviderObjectRef: "connection-primary", SecretRef: "credentials/provider/3",
		SecretVersion: 2, SecretContentSHA256: repeatHex("a"), MaskedAccount: "a***@example.invalid", MaskedLabel: "configured",
		Capabilities: []string{"reasoning", "model-invoke"}, ObservedUsage: 10, ObservedLimit: 100, ObservationRevision: 7,
		ObservedAt: time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC), WindowSeconds: 3600,
		ResetsAt: time.Date(2026, 8, 7, 13, 0, 0, 0, time.UTC), ObservationExpiresAt: time.Date(2026, 8, 7, 12, 5, 0, 0, time.UTC),
		ObservationSHA256: repeatHex("b"),
	}
	base, err := ProviderCredentialMaterializationSHA256(value)
	if err != nil {
		t.Fatal(err)
	}
	changed := value
	changed.CredentialGeneration++
	digest, err := ProviderCredentialMaterializationSHA256(changed)
	if err != nil || digest == base {
		t.Fatalf("credential generation is not bound: %q %v", digest, err)
	}
	changed = value
	changed.ObservationSHA256 = repeatHex("c")
	digest, err = ProviderCredentialMaterializationSHA256(changed)
	if err != nil || digest == base {
		t.Fatalf("capacity observation digest is not bound: %q %v", digest, err)
	}
}

func TestGitIntentBindsTypedCommandAndIgnoresTransportProof(t *testing.T) {
	t.Parallel()

	authority := testAuthority(controlplanev1.ControlPlaneService_ReconcileGitRoleDefinition_FullMethodName)
	request := &controlplanev1.ReconcileGitRoleDefinitionRequest{
		IdempotencyKey: "transport-one", RoleDefinitionId: "", ExpectedVersion: 0, Name: "Role",
		Spec: &controlplanev1.RoleDefinitionSpec{StableKey: "role", Capabilities: []string{"resource.read"}, Ownership: &controlplanev1.ConfigurationOwnership{ManagedBy: controlplanev1.ConfigurationManager_CONFIGURATION_MANAGER_GIT, SourceRef: "https://github.com/codex-k8s/matter-codex#refs/heads/main:path", SourceRevision: 7, SourceSha256: repeatHex("a")}},
	}
	base, err := GitReconciliationIntentSHA256(authority, request)
	if err != nil {
		t.Fatal(err)
	}
	transportChanged := proto.Clone(request).(*controlplanev1.ReconcileGitRoleDefinitionRequest)
	transportChanged.IdempotencyKey = "transport-two"
	transportChanged.ReconciliationReceipt = &controlplanev1.GitReconciliationReceipt{ReceiptId: "ffffffff-ffff-4fff-8fff-ffffffffffff", ReceiptRevision: 88, CommandIntentSha256: repeatHex("f")}
	same, err := GitReconciliationIntentSHA256(authority, transportChanged)
	if err != nil || same != base {
		t.Fatalf("Git transport proof changed semantic hash: %q %q %v", base, same, err)
	}
	mutations := []struct {
		name   string
		mutate func(*controlplanev1.ReconcileGitRoleDefinitionRequest)
	}{
		{"target", func(value *controlplanev1.ReconcileGitRoleDefinitionRequest) {
			value.RoleDefinitionId = "11111111-2222-4333-8444-555555555555"
		}},
		{"version", func(value *controlplanev1.ReconcileGitRoleDefinitionRequest) { value.ExpectedVersion = 2 }},
		{"name", func(value *controlplanev1.ReconcileGitRoleDefinitionRequest) { value.Name = "Role changed" }},
		{"stable-key", func(value *controlplanev1.ReconcileGitRoleDefinitionRequest) { value.Spec.StableKey = "role-changed" }},
		{"typed-spec", func(value *controlplanev1.ReconcileGitRoleDefinitionRequest) {
			value.Spec.Capabilities = append(value.Spec.Capabilities, "resource.write")
		}},
		{"source-ref", func(value *controlplanev1.ReconcileGitRoleDefinitionRequest) {
			value.Spec.Ownership.SourceRef += "-changed"
		}},
		{"source-revision", func(value *controlplanev1.ReconcileGitRoleDefinitionRequest) { value.Spec.Ownership.SourceRevision++ }},
		{"source-digest", func(value *controlplanev1.ReconcileGitRoleDefinitionRequest) {
			value.Spec.Ownership.SourceSha256 = repeatHex("b")
		}},
	}
	for _, item := range mutations {
		t.Run(item.name, func(t *testing.T) {
			changed := proto.Clone(request).(*controlplanev1.ReconcileGitRoleDefinitionRequest)
			item.mutate(changed)
			digest, digestErr := GitReconciliationIntentSHA256(authority, changed)
			if digestErr != nil || digest == base {
				t.Fatalf("Git business field %s is not bound: %q %v", item.name, digest, digestErr)
			}
		})
	}
}

func TestGitAgentIntentBindsEveryReferenceKey(t *testing.T) {
	t.Parallel()

	authority := testAuthority(controlplanev1.ControlPlaneService_ReconcileGitAgent_FullMethodName)
	request := &controlplanev1.ReconcileGitAgentRequest{
		Name: "Agent", RoleDefinitionStableKey: "role", InstructionSetStableKey: "instructions", ProviderPoolStableKey: "pool",
		Spec: &controlplanev1.AgentSpec{StableKey: "agent", Ownership: &controlplanev1.ConfigurationOwnership{ManagedBy: controlplanev1.ConfigurationManager_CONFIGURATION_MANAGER_GIT, SourceRef: "https://github.com/codex-k8s/matter-codex#commit:path", SourceRevision: 1, SourceSha256: repeatHex("a")}},
	}
	base, err := GitReconciliationIntentSHA256(authority, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*controlplanev1.ReconcileGitAgentRequest){
		func(value *controlplanev1.ReconcileGitAgentRequest) { value.RoleDefinitionStableKey += "-changed" },
		func(value *controlplanev1.ReconcileGitAgentRequest) { value.InstructionSetStableKey += "-changed" },
		func(value *controlplanev1.ReconcileGitAgentRequest) { value.ProviderPoolStableKey += "-changed" },
	} {
		changed := proto.Clone(request).(*controlplanev1.ReconcileGitAgentRequest)
		mutate(changed)
		digest, digestErr := GitReconciliationIntentSHA256(authority, changed)
		if digestErr != nil || digest == base {
			t.Fatalf("Git agent reference key is not bound: %q %v", digest, digestErr)
		}
	}
}

func TestSemanticIntentBindsVerifiedFullMethod(t *testing.T) {
	t.Parallel()

	base := semanticBusinessIntent{
		ContractVersion: 1,
		Authority:       testAuthority(controlplanev1.ControlPlaneService_ReconcileGitRoleDefinition_FullMethodName),
		TargetKind:      "role_definition",
		TargetStableKey: "role",
		Action:          "reconcile_git",
		Name:            "Role",
		TypedIntentType: "controlplane.v1.RoleDefinitionSpec",
		TypedIntent:     []byte("stable typed business intent"),
	}
	baseDigest, err := hashSemanticBusinessIntent(base)
	if err != nil {
		t.Fatal(err)
	}
	changed := base
	changed.Authority.FullMethod = controlplanev1.ControlPlaneService_ReconcileGitInstructionSet_FullMethodName
	changedDigest, err := hashSemanticBusinessIntent(changed)
	if err != nil || changedDigest == baseDigest {
		t.Fatalf("verified full method is not bound: %q %v", changedDigest, err)
	}
}

func assertProviderIntentChanged(t *testing.T, base string, authority VerifiedCommandAuthority, request *controlplanev1.ManageProviderConnectionReferenceRequest) {
	t.Helper()
	changed, err := ProviderConnectionReferenceIntentSHA256(authority, request)
	if err != nil || changed == base {
		t.Fatalf("semantic field is not bound: %q %v", changed, err)
	}
}

func testAuthority(fullMethod string) VerifiedCommandAuthority {
	return VerifiedCommandAuthority{ActorID: "11111111-1111-4111-8111-111111111111", OrganizationID: "22222222-2222-4222-8222-222222222222", ProjectID: "33333333-3333-4333-8333-333333333333", WorkloadID: "integration-gateway", FullMethod: fullMethod}
}

func testProviderRequest() *controlplanev1.ManageProviderConnectionReferenceRequest {
	return &controlplanev1.ManageProviderConnectionReferenceRequest{IdempotencyKey: "transport-key", Action: controlplanev1.ProviderConnectionReferenceAction_PROVIDER_CONNECTION_REFERENCE_ACTION_REGISTER, Name: "Provider", Spec: &controlplanev1.ProviderConnectionReferenceSpec{StableKey: "provider-primary", Provider: "openai-codex", ServerReference: "connection-primary", ReferenceVersion: 1, ReferenceGeneration: 1, ReferenceSha256: repeatHex("a"), MaskedLabel: "primary", MaskedStatus: controlplanev1.ProviderConnectionStatus_PROVIDER_CONNECTION_STATUS_AVAILABLE, Capabilities: []string{"model-invoke"}, Eligible: true, ObservedAt: timestamppb.New(time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)), CredentialBindingId: "44444444-4444-4444-8444-444444444444", CredentialBindingVersion: 1, CredentialBindingSha256: repeatHex("d")}}
}

func testProviderPoolRequest() *controlplanev1.ManageProviderPoolRequest {
	observed := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	return &controlplanev1.ManageProviderPoolRequest{
		IdempotencyKey: "transport-key", Action: controlplanev1.ProviderPoolAction_PROVIDER_POOL_ACTION_UPDATE,
		ProviderPoolId: "55555555-5555-4555-8555-555555555555", ExpectedVersion: 3, Name: "Primary pool",
		Spec: &controlplanev1.ProviderPoolSpec{StableKey: "primary-pool", Policy: "LEAST_USED", PolicyRevision: 4,
			EligibilitySnapshotSha256: repeatHex("a"),
			Bindings:                  []*controlplanev1.ProviderPoolBinding{{ProviderConnectionReferenceId: "66666666-6666-4666-8666-666666666666", ProviderConnectionStableKey: "primary-provider", ReferenceVersion: 2, ReferenceSha256: repeatHex("f"), Weight: 100, Eligible: true, ObservedUsage: 25, ObservedLimit: 100, ObservationRevision: 9, ObservedAt: timestamppb.New(observed), ObservationExpiresAt: timestamppb.New(observed.Add(5 * time.Minute)), ObservationSha256: repeatHex("e"), WindowDurationSeconds: 3600, ResetsAt: timestamppb.New(observed.Add(time.Hour))}},
		},
	}
}

func repeatHex(symbol string) string {
	result := ""
	for len(result) < 64 {
		result += symbol
	}
	return result
}
