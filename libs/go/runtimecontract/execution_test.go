package runtimecontract

import (
	"strings"
	"testing"
)

func TestRunnerInputArtifactCatalogIsVersionBoundedAndUnique(t *testing.T) {
	input := validRunnerInputFixture()
	input.InputArtifacts = []RunnerInputArtifact{{
		Ref: "artifact_abcdefgh", FileName: "customer-brief.txt", MediaType: "text/plain",
		Digest: "sha256:" + strings.Repeat("c", 64), SizeBytes: 128, Revision: 1, Version: 2,
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

func validRunnerInputFixture() RunnerInput {
	imageDigest := "sha256:" + strings.Repeat("a", 64)
	return RunnerInput{
		Schema: RunnerInputSchemaV4, Mode: RunnerModeTurn, WorkloadInstance: "runtime-controller-1",
		RunRef: "run_abcdefgh", NodeRef: "node_abcdefgh", SessionRef: "session_abcdefgh",
		TurnRef: "turn_abcdefgh", AgentRef: "agent_abcdefgh", Attempt: 1,
		LeaseRef: "lease_abcdefgh", LeaseFence: "fence-1", LeaseGeneration: 1,
		RuntimeRevisionRef: "revision_abcdefgh", RuntimeRevisionVersion: 1,
		RuntimeRevisionDigest: strings.Repeat("b", 64), ImageReference: "registry.example/roles@" + imageDigest,
		ImageManifestDigest: imageDigest, RoleRuntimeContractRevision: 1,
		RoleRuntimeContractSHA256: strings.Repeat("d", 64), Instructions: "Complete the bounded task.",
		Task: "Prepare the customer response.", Provider: "openai", Model: "codex",
		ProviderAccountRef: "pacc_abcdefgh", ProviderCredentialRef: "pcr_abcdefgh",
		ProviderCredentialRevision: 1, ProviderCredentialSHA256: strings.Repeat("e", 64),
		CodexSandbox: "read-only", CodexApprovalPolicy: "never",
		CallbackURL: "https://10.0.0.10:8444", CallbackTLS: RuntimeTLSBinding{
			ServerName:      "runtime-controller-callback.mattercodex-system.svc.cluster.local",
			CAFile:          "/var/run/config/mattercodex/runtime/callback/ca.crt",
			CertificateFile: "/var/run/secrets/mattercodex/runtime/callback-client/tls.crt",
			PrivateKeyFile:  "/var/run/secrets/mattercodex/runtime/callback-client/tls.key",
		},
		ExecutionTicketFile:    "/var/run/secrets/mattercodex/runtime/ticket/token",
		ProviderAuthFile:       "/var/run/secrets/mattercodex/runtime/provider/auth.json",
		ProviderAuthSHA256File: "/var/run/secrets/mattercodex/runtime/provider/auth.sha256",
		WorkspaceRoot:          "/workspace", OutboxRoot: "/workspace/.matter-codex/outbox", CodexHome: "/tmp/codex-home",
	}
}
