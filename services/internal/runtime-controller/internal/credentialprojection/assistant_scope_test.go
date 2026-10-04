package credentialprojection

import (
	"context"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"strings"
	"testing"
)

type scopeCredentialAPI struct {
	secretbrokerv1.RuntimeCredentialProjectionServiceClient
	input           runtimecontract.RunnerInput
	systemRequests  []*secretbrokerv1.MaterializeSystemAssistantCredentialsRequest
	projectRequests []*secretbrokerv1.MaterializeRuntimeCredentialsRequest
}

func (api *scopeCredentialAPI) MaterializeSystemAssistantCredentials(_ context.Context, request *secretbrokerv1.MaterializeSystemAssistantCredentialsRequest, _ ...grpc.CallOption) (*secretbrokerv1.MaterializeSystemAssistantCredentialsResponse, error) {
	api.systemRequests = append(api.systemRequests, request)
	descriptor := projectionTestDescriptor(api.input)
	descriptor.RuntimeSecretKeys = nil
	return &secretbrokerv1.MaterializeSystemAssistantCredentialsResponse{Projection: descriptor}, nil
}
func (api *scopeCredentialAPI) MaterializeRuntimeCredentials(_ context.Context, request *secretbrokerv1.MaterializeRuntimeCredentialsRequest, _ ...grpc.CallOption) (*secretbrokerv1.MaterializeRuntimeCredentialsResponse, error) {
	api.projectRequests = append(api.projectRequests, request)
	descriptor := projectionTestDescriptor(api.input)
	descriptor.RuntimeSecretKeys = nil
	return &secretbrokerv1.MaterializeRuntimeCredentialsResponse{Projection: descriptor}, nil
}
func TestCredentialMaterializationRoutesByScopeNotScreenContext(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeNone, runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		t.Run(string(scope), func(t *testing.T) {
			input := scopeCredentialInput()
			input.AssistantScope = scope
			if scope == runtimecontract.AssistantScopeProject {
				input.AssistantProfileRef = "asstprof_abcdefgh"
			}
			input.ExecutionBindingDigest, input.MCPBindingDigest, _ = runtimecontract.RuntimeExecutionBindingDigests(input)
			api := &scopeCredentialAPI{input: input}
			client := &Client{api: api}
			if _, err := client.Materialize(t.Context(), input); err != nil {
				t.Fatal(err)
			}
			if scope == runtimecontract.AssistantScopeSystem {
				if len(api.systemRequests) != 1 || len(api.projectRequests) != 0 || !proto.Equal(api.systemRequests[0].Execution, materializeRequest(input)) {
					t.Fatal("system screen context changed organizational credential route")
				}
			} else if len(api.systemRequests) != 0 || len(api.projectRequests) != 1 || !proto.Equal(api.projectRequests[0], materializeRequest(input)) {
				t.Fatal("project runtime obtained organization credential route")
			}
		})
	}
}
func TestCredentialMaterializationRejectsUnknownAndDetachedScopeBeforeRPC(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{"", "ADMIN", runtimecontract.AssistantScopeProject} {
		input := scopeCredentialInput()
		input.AssistantScope = scope
		api := &scopeCredentialAPI{input: input}
		if _, err := (&Client{api: api}).Materialize(t.Context(), input); err == nil || len(api.systemRequests)+len(api.projectRequests) != 0 {
			t.Fatal("invalid assistant scope reached credential RPC")
		}
	}
}
func scopeCredentialInput() runtimecontract.RunnerInput {
	digest := "sha256:" + strings.Repeat("a", 64)
	image := runtimecontract.RuntimeEnvironmentImage{ArtifactRef: "imgart_abcdefgh", RecipeRef: "imgrec_abcdefgh", RecipeGeneration: 1, Reference: "registry.example/runner@" + digest, Digest: digest}
	policy := runtimecontract.DefaultRuntimeEnvironmentPolicy()
	access, _ := runtimecontract.RuntimeKubernetesAccessForExecution(policy.KubernetesAccess, "agent-runner", "system-assistant-warm")
	environmentDigest, _ := runtimecontract.RuntimeEnvironmentDigest(nil, nil, image, nil, policy)
	input := runtimecontract.RunnerInput{
		Schema: runtimecontract.RunnerInputSchemaV8, Mode: runtimecontract.RunnerModeTurn,
		OrganizationRef:  "org_abcdefgh",
		WorkloadInstance: "runtime-controller", RunRef: "run_abcdefgh", NodeRef: "node_abcdefgh",
		ProjectRef: "prj_abcdefgh", SessionRef: "session_abcdefgh", TurnRef: "turn_abcdefgh", AgentRef: "agent_abcdefgh", Attempt: 1,
		LeaseRef: "lease_abcdefgh", LeaseFence: "fence", LeaseGeneration: 1,
		InputDigest:        strings.Repeat("0", 64),
		RuntimeRevisionRef: "revision_abcdefgh", RuntimeRevisionVersion: 1,
		RuntimeRevisionDigest: strings.Repeat("b", 64), ImageReference: image.Reference,
		ImageManifestDigest: digest, EnvironmentImage: image, RoleRuntimeContractRevision: 1,
		RoleRuntimeContractSHA256: strings.Repeat("c", 64), RoleDefinitionRef: "roledef_abcdefgh",
		RuntimeProfileRef: "profile_abcdefgh", RuntimeProfileRevision: "profile-revision-1",
		InstructionRef: "instr_abcdefgh", InstructionDigest: strings.Repeat("5", 64),
		PromptTemplateRef: "prompt_abcdefgh", PromptTemplateDigest: strings.Repeat("6", 64),
		PromptMaterializationDigest: strings.Repeat("7", 64), AssistantScope: runtimecontract.AssistantScopeSystem,
		Instructions: "Complete the task.", Task: "Prepare the result.", Provider: "openai-codex", Model: "codex",
		ProviderAccountRef: "pacc_abcdefgh", ProviderCredentialRef: "pcr_abcdefgh",
		ProviderCredentialRevision: 1, ProviderCredentialSHA256: strings.Repeat("d", 64),
		RuntimeConfigRef: "rconf_abcdefgh", RuntimeConfigVersion: 1, RuntimeConfigDigest: strings.Repeat("1", 64),
		ProviderPolicyRef: "ppol_abcdefgh", ProviderPolicyVersion: 1, ProviderPolicyDigest: strings.Repeat("2", 64),
		ConfigOverlayRef: "cover_abcdefgh", ConfigOverlayVersion: 1,
		ReasoningMode: runtimecontract.ReasoningSupported, EffectiveReasoningEffort: "medium",
		ConfigOverlayDigest:   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		RuntimeEnvironmentRef: "renv_abcdefgh", RuntimeEnvironmentVersion: 1,
		RuntimeEnvironmentDigest: environmentDigest,
		EnvironmentPolicy:        policy, WorkspacePolicy: runtimecontract.RuntimeWorkspacePolicyV1(),
		EffectiveKubernetesAccess: access,
		EnvironmentBindingRef:     "aenv_abcdefgh", EnvironmentBindingVersion: 1, EnvironmentBindingDigest: strings.Repeat("3", 64),
		CodexSandbox: "read-only", CodexApprovalPolicy: "never",
		CallbackURL: "https://10.0.0.10:8444", CallbackTLS: runtimecontract.RuntimeTLSBinding{
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
	input.InputDigest, _ = runtimecontract.RuntimeBoundedInputDigest(input.BoundedInput)
	input.ExecutionBindingDigest, input.MCPBindingDigest, _ = runtimecontract.RuntimeExecutionBindingDigests(input)
	return input
}
