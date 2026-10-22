// Package testfixture supplies synthetic runtime-controller inputs shared only by unit tests.
// Пакет testfixture предоставляет синтетические входы runtime-controller только для unit-тестов.
package testfixture

import (
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TurnInput builds a fresh synthetic turn with pinned environment, image and binding digests.
// Identities, credential hashes and mount paths are inert fixture metadata: this factory
// does not create files, grant authority, contact a provider or start a workload.
// TurnInput создаёт новый синтетический ход с закреплёнными окружением, образом и дайджестами привязок.
// Идентификаторы, хеши учётных данных и пути — неактивные тестовые данные: фабрика
// не создаёт файлы, не выдаёт полномочия, не обращается к провайдеру и не запускает нагрузку.
func TurnInput() runtimecontract.RunnerInput {
	digest := "sha256:" + strings.Repeat("a", 64)
	image := runtimecontract.RuntimeEnvironmentImage{ArtifactRef: "imgart_abcdefgh", RecipeRef: "imgrec_abcdefgh", RecipeGeneration: 1, Reference: "registry.example/runner@" + digest, Digest: digest}
	policy := runtimecontract.DefaultRuntimeEnvironmentPolicy()
	access, _ := runtimecontract.RuntimeKubernetesAccessForExecution(policy.KubernetesAccess, "agent-runner", "system-assistant-warm")
	environmentDigest, _ := runtimecontract.RuntimeEnvironmentDigest(nil, nil, image, nil, policy)
	input := runtimecontract.RunnerInput{
		Schema: runtimecontract.RunnerInputSchemaV9, Mode: runtimecontract.RunnerModeTurn,
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
