package runtimecontract

import (
	"strings"
	"testing"
)

func TestRunnerInputArtifactCatalogIsVersionBoundedAndUnique(t *testing.T) {
	input := validRunnerInputFixture()
	input.Capabilities = []string{ArtifactCapability}
	input.AttachmentSetRef = "aset_abcdefgh"
	input.AttachmentSetManifestDigest = strings.Repeat("4", 64)
	input.AttachmentContext = "RUN_INPUT"
	input.AttachmentSets = []RunnerAttachmentSet{{
		Ref: input.AttachmentSetRef, ManifestDigest: input.AttachmentSetManifestDigest,
		Purpose: input.AttachmentContext, Scope: AttachmentScopeInput, Provenance: "CURRENT_TURN", TurnRef: input.TurnRef,
	}}
	input.InputArtifacts = []RunnerInputArtifact{{
		Ref: "artifact_abcdefgh", FileName: "customer-brief.txt", MediaType: "text/plain",
		Digest: "sha256:" + strings.Repeat("c", 64), SizeBytes: 128, Revision: 1, Version: 2,
		Scope: "INPUT", Position: 1, Source: "CONTROL_CENTER", AttachmentSetRef: input.AttachmentSetRef,
		AttachmentPurpose: input.AttachmentContext, Provenance: "CURRENT_TURN",
	}}
	if _, err := EncodeRunnerInput(input); err != nil {
		t.Fatalf("EncodeRunnerInput() rejected a valid artifact catalog: %v", err)
	}
	input.InputArtifacts = append(input.InputArtifacts, input.InputArtifacts[0])
	if _, err := EncodeRunnerInput(input); err == nil {
		t.Fatal("EncodeRunnerInput() accepted duplicate artifact refs")
	}
	input.InputArtifacts = input.InputArtifacts[:1]
	input.InputArtifacts[0].FileName = "../secret"
	if _, err := EncodeRunnerInput(input); err == nil {
		t.Fatal("EncodeRunnerInput() accepted an unsafe artifact filename")
	}
}

func TestWarmCompatibilityDigestIgnoresTurnIdentityAndRejectsRuntimeDrift(t *testing.T) {
	turn := validRunnerInputFixture()
	turn.SystemAssistant = true
	warm := turn
	warm.Mode = RunnerModeWarm
	warm.RunRef, warm.NodeRef, warm.TurnRef = "", "", ""
	warm.Attempt, warm.LeaseRef, warm.LeaseFence, warm.LeaseGeneration = 0, "", "", 0
	warm.Task = ""
	warm.RuntimeRevisionDigest = strings.Repeat("f", 64)

	warmDigest, err := WarmCompatibilityDigest(warm)
	if err != nil {
		t.Fatalf("WarmCompatibilityDigest(warm) error = %v", err)
	}
	turnDigest, err := WarmCompatibilityDigest(turn)
	if err != nil {
		t.Fatalf("WarmCompatibilityDigest(turn) error = %v", err)
	}
	if warmDigest != turnDigest {
		t.Fatalf("compatible warm and turn digests differ: %s != %s", warmDigest, turnDigest)
	}

	turn.Model = "different-model"
	driftedDigest, err := WarmCompatibilityDigest(turn)
	if err != nil {
		t.Fatalf("WarmCompatibilityDigest(drifted turn) error = %v", err)
	}
	if driftedDigest == warmDigest {
		t.Fatal("runtime drift retained the warm compatibility digest")
	}
}

func TestRunnerInputAcceptsImmutableSystemRuntimeRevision(t *testing.T) {
	input := validRunnerInputFixture()
	input.RuntimeRevisionRef = "system-assistant-runtime-" + strings.Repeat("a", 64)
	if err := input.Validate(); err != nil {
		t.Fatalf("immutable system runtime revision rejected: %v", err)
	}
	input.RuntimeRevisionRef = "system-assistant-runtime-latest"
	if err := input.Validate(); err == nil {
		t.Fatal("mutable system runtime revision accepted")
	}
}

func TestRunnerInputNestedCatalogMatchesV6SchemaBoundary(t *testing.T) {
	valid := validRunnerInputFixture()
	valid.SessionContext = []RunnerSessionMessage{{Role: "USER", Content: "bounded context"}}
	valid.IntegrationGrants = []RunnerIntegrationGrant{{
		Ref: "grant_abcdefgh", ConnectionRef: "conn_abcdefgh", DefinitionKey: "crm",
		ConnectionName: "CRM", CapabilityKey: "crm.read", CapabilityName: "Read CRM",
		CapabilityDescription: "Read bounded CRM records.", Risk: "READ",
	}}
	valid.Capabilities = []string{"crm.read"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid nested runner catalog rejected: %v", err)
	}
	for name, mutate := range map[string]func(*RunnerInput){
		"session role": func(input *RunnerInput) { input.SessionContext[0].Role = "OWNER" },
		"grant risk":   func(input *RunnerInput) { input.IntegrationGrants[0].Risk = "CRITICAL" },
		"duplicate capability": func(input *RunnerInput) {
			input.Capabilities = append(input.Capabilities, input.Capabilities[0])
		},
	} {
		t.Run(name, func(t *testing.T) {
			input := valid
			input.SessionContext = append([]RunnerSessionMessage(nil), valid.SessionContext...)
			input.IntegrationGrants = append([]RunnerIntegrationGrant(nil), valid.IntegrationGrants...)
			input.Capabilities = append([]string(nil), valid.Capabilities...)
			mutate(&input)
			if input.Validate() == nil {
				t.Fatalf("invalid nested runner catalog accepted: %#v", input)
			}
		})
	}
}

func TestTokenUsageValidationRejectsInconsistentCounters(t *testing.T) {
	valid := TokenUsage{TotalTokens: 120, InputTokens: 100, CachedInputTokens: 40,
		CacheWriteInputTokens: 10, OutputTokens: 20, ReasoningOutputTokens: 5, ModelContextWindow: 200000}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid usage rejected: %v", err)
	}
	for name, usage := range map[string]TokenUsage{
		"negative":          {TotalTokens: -1},
		"total mismatch":    {TotalTokens: 121, InputTokens: 100, OutputTokens: 20},
		"cached over input": {TotalTokens: 120, InputTokens: 100, CachedInputTokens: 101, OutputTokens: 20},
		"reasoning over output": {
			TotalTokens: 120, InputTokens: 100, OutputTokens: 20, ReasoningOutputTokens: 21,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if usage.Validate() == nil {
				t.Fatalf("invalid usage accepted: %#v", usage)
			}
		})
	}
}

func TestRunnerCompletionArchiveBindingIsCompleteAndBounded(t *testing.T) {
	request := RunnerCompletionRequest{RuntimeRevisionDigest: strings.Repeat("b", 64), Success: true,
		ResultSummary: "done", CodexSessionID: "00000000-0000-4000-8000-000000000001",
		ArchiveRelativePath: ".kodex/state/codex-home/sessions/2026/08/28/rollout-2026-08-28T23-23-39-00000000-0000-4000-8000-000000000001.jsonl",
		ArchiveSHA256:       strings.Repeat("c", 64), ArchiveSizeBytes: 1024}
	if err := request.Validate(); err != nil {
		t.Fatalf("valid archive binding rejected: %v", err)
	}
	request.ArchiveRelativePath = ".kodex/state/codex-home/sessions/2026/08/28/rollout-2026-08-28T23-23-39-00000000-0000-4000-8000-000000000002.jsonl"
	if err := request.Validate(); err == nil {
		t.Fatal("archive bound to another Codex session accepted")
	}
	request.ArchiveRelativePath = ".kodex/state/codex-home/sessions/2026/08/28/rollout-2026-08-28T23-23-39-00000000-0000-4000-8000-000000000001.jsonl"
	request.ArchiveSizeBytes = 0
	if err := request.Validate(); err == nil {
		t.Fatal("incomplete archive binding accepted")
	}
}

func TestProviderCredentialRefreshRequestIsExactAndBounded(t *testing.T) {
	valid := RunnerProviderCredentialRefreshRequest{
		RuntimeRevisionDigest:         strings.Repeat("a", 64),
		PreviousCredentialRevisionRef: "pcr_abcdefgh",
		PreviousContentSHA256:         strings.Repeat("b", 64),
		Authentication:                []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"redacted"}}`),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid provider credential refresh rejected: %v", err)
	}
	for name, mutate := range map[string]func(*RunnerProviderCredentialRefreshRequest){
		"invalid revision digest": func(request *RunnerProviderCredentialRefreshRequest) {
			request.RuntimeRevisionDigest = "invalid"
		},
		"invalid credential ref": func(request *RunnerProviderCredentialRefreshRequest) {
			request.PreviousCredentialRevisionRef = "../credential"
		},
		"invalid previous digest": func(request *RunnerProviderCredentialRefreshRequest) {
			request.PreviousContentSHA256 = "invalid"
		},
		"invalid authentication": func(request *RunnerProviderCredentialRefreshRequest) {
			request.Authentication = []byte("not-json")
		},
		"oversized authentication": func(request *RunnerProviderCredentialRefreshRequest) {
			request.Authentication = append([]byte{'{'}, make([]byte, MaximumProviderAuthBytes)...)
		},
	} {
		t.Run(name, func(t *testing.T) {
			request := valid
			request.Authentication = append([]byte(nil), valid.Authentication...)
			mutate(&request)
			if request.Validate() == nil {
				t.Fatalf("invalid provider credential refresh accepted: %#v", request)
			}
		})
	}
}

func TestSessionPVCNameIsStableAndRejectsInvalidReference(t *testing.T) {
	first, err := SessionPVCName("ses_abcdefgh")
	if err != nil {
		t.Fatalf("SessionPVCName() error = %v", err)
	}
	second, _ := SessionPVCName("ses_abcdefgh")
	if first != second || !strings.HasPrefix(first, "runtime-session-") || len(first) != len("runtime-session-")+16 {
		t.Fatalf("SessionPVCName() = %q, %q", first, second)
	}
	if _, err := SessionPVCName("../session"); err == nil {
		t.Fatal("SessionPVCName() accepted an invalid reference")
	}
}

func validRunnerInputFixture() RunnerInput {
	imageDigest := "sha256:" + strings.Repeat("a", 64)
	image := RuntimeEnvironmentImage{ArtifactRef: "imgart_abcdefgh", RecipeRef: "imgrec_abcdefgh",
		RecipeGeneration: 1, Reference: "registry.example/roles@" + imageDigest, Digest: imageDigest}
	policy := DefaultRuntimeEnvironmentPolicy()
	environmentDigest, _ := RuntimeEnvironmentDigest(nil, nil, image, nil, policy)
	input := RunnerInput{
		Schema: RunnerInputSchemaV6, Mode: RunnerModeTurn, WorkloadInstance: "runtime-controller-1",
		RunRef: "run_abcdefgh", NodeRef: "node_abcdefgh", SessionRef: "session_abcdefgh",
		TurnRef: "turn_abcdefgh", AgentRef: "agent_abcdefgh", Attempt: 1,
		LeaseRef: "lease_abcdefgh", LeaseFence: "fence-1", LeaseGeneration: 1,
		RuntimeRevisionRef: "revision_abcdefgh", RuntimeRevisionVersion: 1,
		RuntimeRevisionDigest: strings.Repeat("b", 64), ImageReference: "registry.example/roles@" + imageDigest,
		ImageManifestDigest: imageDigest, EnvironmentImage: image, RoleRuntimeContractRevision: 1,
		RoleRuntimeContractSHA256: strings.Repeat("d", 64), Instructions: "Complete the bounded task.",
		Task: "Prepare the customer response.", Provider: "openai", Model: "codex",
		ProviderAccountRef: "pacc_abcdefgh", ProviderCredentialRef: "pcr_abcdefgh",
		ProviderCredentialRevision: 1, ProviderCredentialSHA256: strings.Repeat("e", 64),
		RuntimeConfigRef: "rconf_abcdefgh", RuntimeConfigVersion: 1, RuntimeConfigDigest: strings.Repeat("1", 64),
		ProviderPolicyRef: "ppol_abcdefgh", ProviderPolicyVersion: 1, ProviderPolicyDigest: strings.Repeat("2", 64),
		ConfigOverlayRef: "cov_abcdefgh", ConfigOverlayVersion: 1,
		ConfigOverlayDigest:   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		RuntimeEnvironmentRef: "renv_abcdefgh", RuntimeEnvironmentVersion: 1,
		RuntimeEnvironmentDigest: environmentDigest,
		EnvironmentPolicy:        policy,
		EnvironmentBindingRef:    "aenv_abcdefgh", EnvironmentBindingVersion: 1, EnvironmentBindingDigest: strings.Repeat("3", 64),
		CodexSandbox: "read-only", CodexApprovalPolicy: "never",
		CallbackURL: "https://10.0.0.10:8444", CallbackTLS: RuntimeTLSBinding{
			ServerName:      "runtime-controller-callback.kodex-system.svc.cluster.local",
			CAFile:          "/var/run/config/kodex/runtime/callback/ca.crt",
			CertificateFile: "/var/run/secrets/kodex/runtime/callback-client/tls.crt",
			PrivateKeyFile:  "/var/run/secrets/kodex/runtime/callback-client/tls.key",
		},
		ExecutionTicketFile:    "/var/run/secrets/kodex/runtime/ticket/token",
		ProviderAuthFile:       "/run/secrets/kodex/runtime/provider/auth.json",
		ProviderAuthSHA256File: "/run/secrets/kodex/runtime/provider/auth.sha256",
		WorkspaceRoot:          "/workspace", OutboxRoot: "/workspace/.kodex/outbox", CodexHome: "/workspace/.kodex/state/codex-home",
	}
	input.EffectiveKubernetesAccess, _ = RuntimeKubernetesAccessForExecution(policy.KubernetesAccess,
		RuntimeServiceAccountName(input.LeaseRef), RuntimeTurnPodName(input.LeaseRef))
	return input
}
