package callback

import (
	"context"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestCoordinatorMatchesWarmExecutionByCompatibilityDigest(t *testing.T) {
	t.Parallel()
	coordinator := NewCoordinator()
	input := validWarmExecutionInput()
	compatibility := strings.Repeat("a", 64)
	if err := coordinator.EnqueueWarm(input, compatibility); err != nil {
		t.Fatalf("EnqueueWarm() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, available := coordinator.NextWarm(ctx, strings.Repeat("b", 64)); available {
		t.Fatal("incompatible warm runtime received execution")
	}
}

func TestCoordinatorReturnsCompatibleWarmExecution(t *testing.T) {
	t.Parallel()
	coordinator := NewCoordinator()
	input := validWarmExecutionInput()
	compatibility := strings.Repeat("a", 64)
	if err := coordinator.EnqueueWarm(input, compatibility); err != nil {
		t.Fatalf("EnqueueWarm() error = %v", err)
	}
	result, available := coordinator.NextWarm(t.Context(), compatibility)
	if !available || result.LeaseRef != input.LeaseRef {
		t.Fatalf("NextWarm() = %#v, %t", result, available)
	}
}

func validWarmExecutionInput() runtimecontract.RunnerInput {
	digest := "sha256:" + strings.Repeat("a", 64)
	return runtimecontract.RunnerInput{
		Schema: runtimecontract.RunnerInputSchemaV6, Mode: runtimecontract.RunnerModeTurn,
		WorkloadInstance: "runtime-controller", RunRef: "run_abcdefgh", NodeRef: "node_abcdefgh",
		SessionRef: "session_abcdefgh", TurnRef: "turn_abcdefgh", AgentRef: "agent_abcdefgh", Attempt: 1,
		LeaseRef: "lease_abcdefgh", LeaseFence: "fence", LeaseGeneration: 1,
		RuntimeRevisionRef: "revision_abcdefgh", RuntimeRevisionVersion: 1,
		RuntimeRevisionDigest: strings.Repeat("b", 64), ImageReference: "registry.example/runner@" + digest,
		ImageManifestDigest: digest, RoleRuntimeContractRevision: 1,
		RoleRuntimeContractSHA256: strings.Repeat("c", 64), SystemAssistant: true,
		Instructions: "Complete the task.", Task: "Prepare the result.", Provider: "openai-codex", Model: "codex",
		ProviderAccountRef: "pacc_abcdefgh", ProviderCredentialRef: "pcr_abcdefgh",
		ProviderCredentialRevision: 1, ProviderCredentialSHA256: strings.Repeat("d", 64),
		RuntimeConfigRef: "rconf_abcdefgh", RuntimeConfigVersion: 1, RuntimeConfigDigest: strings.Repeat("1", 64),
		ProviderPolicyRef: "ppol_abcdefgh", ProviderPolicyVersion: 1, ProviderPolicyDigest: strings.Repeat("2", 64),
		ConfigOverlayRef: "cover_abcdefgh", ConfigOverlayVersion: 1,
		ConfigOverlayDigest:   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		RuntimeEnvironmentRef: "renv_abcdefgh", RuntimeEnvironmentVersion: 1,
		RuntimeEnvironmentDigest: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		EnvironmentBindingRef:    "aenv_abcdefgh", EnvironmentBindingVersion: 1, EnvironmentBindingDigest: strings.Repeat("3", 64),
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
}
