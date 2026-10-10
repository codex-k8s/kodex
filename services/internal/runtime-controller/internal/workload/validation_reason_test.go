package workload

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"k8s.io/client-go/kubernetes/fake"
)

func TestTurnInputValidationReasonClosedProjection(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"REQUIRED_CONTEXT", "ATTACHMENT_LINEAGE", "ATTACHMENT_MANIFEST", "WORKSPACE_MANIFEST"} {
		t.Run(reason, func(t *testing.T) {
			cause := errors.New("synthetic private input must not enter reason")
			failure := turnInputBuildError("validation", validationFailure(reason, cause))
			var typed *TurnInputBuildError
			if !errors.As(failure, &typed) || typed.ValidationReason() != reason || !errors.Is(failure, cause) {
				t.Fatal("closed reason or original cause was lost")
			}
			if failure.Error() != "runtime turn input build failed" {
				t.Fatal("public failure text changed")
			}
		})
	}
	for _, failure := range []*TurnInputBuildError{
		nil,
		{Stage: "base", Cause: validationFailure("REQUIRED_CONTEXT", runtimecontract.ErrRuntimeContext)},
		{Stage: "validation", Cause: errors.New("synthetic-private")},
		{Stage: "validation", Cause: validationFailure("synthetic-private", errors.New("runner input is invalid"))},
		{Stage: "validation", Cause: validationFailure("RUNNER_INPUT", fmt.Errorf("synthetic-private: %w", errors.New("runner input is invalid")))},
	} {
		if failure.ValidationReason() != "UNKNOWN" {
			t.Fatal("unknown diagnostic was not closed")
		}
	}
}

func TestRuntimeEnvelopeReasonMatchesActualValidator(t *testing.T) {
	manager := newTestManager(t, fake.NewSimpleClientset())
	baseline, _, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatal(err)
	}
	// Только oracle fixture: необязательный context и semantic envelope исключены,
	// чтобы предшествующие guards не скрывали проверяемую OR-ветку.
	baseline.ContextSnapshot = nil
	baseline.PromptServiceTemplateRevision = ""
	baseline.PromptServiceTemplateDigest = ""
	baseline.PromptTargetKind = ""
	baseline.ExecutionBindingDigest, baseline.MCPBindingDigest, err = runtimecontract.RuntimeExecutionBindingDigests(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Validate() != nil || runtimeEnvelopeReason(baseline) != "UNKNOWN" {
		t.Fatal("valid baseline changed")
	}
	tests := map[string]func(*runtimecontract.RunnerInput){
		"ENVELOPE_SCHEMA":                 func(input *runtimecontract.RunnerInput) { input.Schema = "" },
		"ENVELOPE_MODE":                   func(input *runtimecontract.RunnerInput) { input.Mode = "" },
		"ENVELOPE_WORKLOAD":               func(input *runtimecontract.RunnerInput) { input.WorkloadInstance = "" },
		"ENVELOPE_ORGANIZATION_REF":       func(input *runtimecontract.RunnerInput) { input.OrganizationRef = "" },
		"ENVELOPE_SESSION_REF":            func(input *runtimecontract.RunnerInput) { input.SessionRef = "" },
		"ENVELOPE_AGENT_REF":              func(input *runtimecontract.RunnerInput) { input.AgentRef = "" },
		"ENVELOPE_REVISION_REF":           func(input *runtimecontract.RunnerInput) { input.RuntimeRevisionRef = "" },
		"ENVELOPE_REVISION_VERSION":       func(input *runtimecontract.RunnerInput) { input.RuntimeRevisionVersion = 0 },
		"ENVELOPE_REVISION_DIGEST":        func(input *runtimecontract.RunnerInput) { input.RuntimeRevisionDigest = "" },
		"ENVELOPE_IMAGE":                  func(input *runtimecontract.RunnerInput) { input.ImageManifestDigest = "" },
		"ENVELOPE_ROLE_CONTRACT":          func(input *runtimecontract.RunnerInput) { input.RoleRuntimeContractRevision = 0 },
		"ENVELOPE_ROLE_DEFINITION_REF":    func(input *runtimecontract.RunnerInput) { input.RoleDefinitionRef = "" },
		"ENVELOPE_RUNTIME_PROFILE":        func(input *runtimecontract.RunnerInput) { input.RuntimeProfileRef = "invalid" },
		"ENVELOPE_INSTRUCTION_BINDING":    func(input *runtimecontract.RunnerInput) { input.InstructionRef = "" },
		"ENVELOPE_PROMPT_TEMPLATE":        func(input *runtimecontract.RunnerInput) { input.PromptTemplateRef = "" },
		"ENVELOPE_PROMPT_MATERIALIZATION": func(input *runtimecontract.RunnerInput) { input.PromptMaterializationDigest = "" },
		"ENVELOPE_INSTRUCTIONS":           func(input *runtimecontract.RunnerInput) { input.Instructions = " " },
		"ENVELOPE_PROVIDER_MODEL":         func(input *runtimecontract.RunnerInput) { input.Model = "" },
		"ENVELOPE_PROVIDER_ACCOUNT_REF":   func(input *runtimecontract.RunnerInput) { input.ProviderAccountRef = "" },
		"ENVELOPE_PROVIDER_CREDENTIAL":    func(input *runtimecontract.RunnerInput) { input.ProviderCredentialRevision = 0 },
		"ENVELOPE_RUNTIME_CONFIGURATION":  func(input *runtimecontract.RunnerInput) { input.RuntimeConfigVersion = 0 },
		"ENVELOPE_PROVIDER_POLICY":        func(input *runtimecontract.RunnerInput) { input.ProviderPolicyVersion = 0 },
		"ENVELOPE_CONFIG_OVERLAY":         func(input *runtimecontract.RunnerInput) { input.ConfigOverlayVersion = 0 },
		"ENVELOPE_RUNTIME_ENVIRONMENT":    func(input *runtimecontract.RunnerInput) { input.RuntimeEnvironmentVersion = 0 },
		"ENVELOPE_ENVIRONMENT_BINDING":    func(input *runtimecontract.RunnerInput) { input.EnvironmentBindingVersion = 0 },
		"ENVELOPE_CODEX_SANDBOX":          func(input *runtimecontract.RunnerInput) { input.CodexSandbox = "" },
		"ENVELOPE_CODEX_APPROVAL_POLICY":  func(input *runtimecontract.RunnerInput) { input.CodexApprovalPolicy = "" },
		"ENVELOPE_CODEX_SESSION": func(input *runtimecontract.RunnerInput) {
			input.CodexSessionID = "00000000-0000-0000-0000-000000000000"
		},
		"ENVELOPE_CALLBACK_TRANSPORT": func(input *runtimecontract.RunnerInput) { input.CallbackURL = "" },
		"ENVELOPE_SECRET_PATHS":       func(input *runtimecontract.RunnerInput) { input.ProviderAuthFile = "/synthetic-private" },
		"ENVELOPE_WORKSPACE_PATHS":    func(input *runtimecontract.RunnerInput) { input.WorkspaceRoot = "/synthetic-private" },
		"ENVELOPE_COLLECTION_LIMITS": func(input *runtimecontract.RunnerInput) {
			input.SessionContext = make([]runtimecontract.RunnerSessionMessage, 129)
		},
		"ENVELOPE_SESSION_USER_CONTENT": func(input *runtimecontract.RunnerInput) {
			input.SessionContext = []runtimecontract.RunnerSessionMessage{{Role: "USER", Content: ""}}
		},
		"ENVELOPE_SESSION_CONTEXT": func(input *runtimecontract.RunnerInput) {
			input.SessionContext = []runtimecontract.RunnerSessionMessage{{Role: "SYNTHETIC_PRIVATE", Content: "data"}}
		},
		"ENVELOPE_CAPABILITIES": func(input *runtimecontract.RunnerInput) { input.Capabilities = []string{"duplicate", "duplicate"} },
		"ENVELOPE_ENVIRONMENT_VALUES": func(input *runtimecontract.RunnerInput) {
			input.EnvironmentValues = []runtimecontract.RuntimeEnvironmentValue{{Name: "bad-name", Value: "synthetic-private"}}
		},
		"ENVELOPE_ENVIRONMENT_IMAGE": func(input *runtimecontract.RunnerInput) { input.EnvironmentImage.ArtifactRef = "" },
	}
	for want, mutate := range tests {
		t.Run(want, func(t *testing.T) {
			input := baseline
			mutate(&input)
			actual := input.Validate()
			if actual == nil || actual.Error() != "runner input is invalid" {
				t.Fatal("mutation did not reach actual envelope guard")
			}
			got := runtimeEnvelopeReason(input)
			if got != want || !knownEnvelopeReason(got) {
				t.Fatalf("reason=%s want=%s", got, want)
			}
			failure := &TurnInputBuildError{Stage: "validation", Cause: &runnerInputValidationFailure{reason: "RUNNER_INPUT", cause: actual, envelopeReason: got}}
			if failure.ValidationReason() != want || !errors.Is(failure, actual) {
				t.Fatal("projection changed cause or code")
			}
		})
	}
	for _, reason := range []string{"ENVELOPE_synthetic-private", "UNKNOWN", "ENVELOPE"} {
		if knownEnvelopeReason(reason) {
			t.Fatal("arbitrary reason accepted")
		}
	}
}

func TestRuntimeEnvelopeIntegrationReasonsMatchActualValidator(t *testing.T) {
	manager := newTestManager(t, fake.NewSimpleClientset())
	baseline, _, err := manager.BuildTurnInput(testExecution(false))
	if err != nil {
		t.Fatal(err)
	}
	baseline.ContextSnapshot = nil
	baseline.PromptServiceTemplateRevision = ""
	baseline.PromptServiceTemplateDigest = ""
	baseline.PromptTargetKind = ""
	schema := `{"type":"object","additionalProperties":false}`
	digest := sha256.Sum256([]byte(schema))
	grant := runtimecontract.RunnerIntegrationGrant{
		Ref: "grt_fixture001", GrantVersion: 1, ConnectionRef: "int_fixture001", ConnectionVersion: 1,
		ApprovalPolicy: "NONE", DefinitionKey: "synthetic", ConnectionName: "Synthetic",
		CapabilityKey: baseline.Capabilities[0], CapabilityName: "Synthetic", Risk: "READ",
		DefinitionVersion: "1.0.0", DefinitionDigest: strings.Repeat("a", 64),
		Operation: "READ", InputSchema: schema, InputSchemaSHA256: hex.EncodeToString(digest[:]),
	}
	baseline.IntegrationGrants = []runtimecontract.RunnerIntegrationGrant{grant}
	baseline.ExecutionBindingDigest, baseline.MCPBindingDigest, err = runtimecontract.RuntimeExecutionBindingDigests(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Validate() != nil || runtimeEnvelopeReason(baseline) != "UNKNOWN" {
		t.Fatal("valid grant baseline changed")
	}
	for want, mutate := range map[string]func(*runtimecontract.RunnerInput){
		"ENVELOPE_INTEGRATION_REFS":     func(input *runtimecontract.RunnerInput) { input.IntegrationGrants[0].GrantVersion = 0 },
		"ENVELOPE_INTEGRATION_APPROVAL": func(input *runtimecontract.RunnerInput) { input.IntegrationGrants[0].ApprovalPolicy = "HUMAN_SCOPED" },
		"ENVELOPE_INTEGRATION_TEXT": func(input *runtimecontract.RunnerInput) {
			input.IntegrationGrants[0].CapabilityDescription = strings.Repeat("я", 1001)
		},
		"ENVELOPE_INTEGRATION_PACKAGE": func(input *runtimecontract.RunnerInput) { input.IntegrationGrants[0].DefinitionVersion = "invalid" },
		"ENVELOPE_INTEGRATION_SCHEMA":  func(input *runtimecontract.RunnerInput) { input.IntegrationGrants[0].InputSchema = `{"type":"object"}` },
		"ENVELOPE_INTEGRATION_RISK":    func(input *runtimecontract.RunnerInput) { input.IntegrationGrants[0].Risk = "SYNTHETIC_PRIVATE" },
		"ENVELOPE_INTEGRATION_DUPLICATE": func(input *runtimecontract.RunnerInput) {
			input.IntegrationGrants = append(input.IntegrationGrants, input.IntegrationGrants[0])
		},
	} {
		t.Run(want, func(t *testing.T) {
			input := baseline
			input.IntegrationGrants = append([]runtimecontract.RunnerIntegrationGrant(nil), baseline.IntegrationGrants...)
			mutate(&input)
			actual := input.Validate()
			if actual == nil || actual.Error() != "runner input is invalid" {
				t.Fatal("mutation did not reach actual envelope guard")
			}
			if got := runtimeEnvelopeReason(input); got != want {
				t.Fatalf("reason=%s want=%s", got, want)
			}
		})
	}
}

func TestEnvelopeDiagnosticCannotOverrideDifferentFailure(t *testing.T) {
	cause := errors.New("managed MCP profile binding is invalid")
	for _, detail := range []string{"synthetic-private", "ENVELOPE_SCHEMA"} {
		failure := &TurnInputBuildError{Stage: "validation", Cause: &runnerInputValidationFailure{reason: "RUNNER_INPUT", cause: cause, envelopeReason: detail}}
		if failure.ValidationReason() != "MANAGED_MCP_PROFILE" {
			t.Fatal("reason replaced a different validator classification")
		}
	}
}

func TestRunnerInputValidationReasonKnownBranches(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]string{
		"runtime prompt service materialization is invalid":          "PROMPT_SERVICE",
		"runner assistant scope binding is invalid":                  "ASSISTANT_SCOPE",
		"runner input is invalid":                                    "ENVELOPE",
		"managed MCP profile binding is invalid":                     "MANAGED_MCP_PROFILE",
		"required managed MCP health proof is unavailable":           "MANAGED_MCP_HEALTH",
		"runtime execution deadline is invalid":                      "EXECUTION_DEADLINE",
		"runtime execution clock is invalid":                         "EXECUTION_DEADLINE",
		"runtime effective execution deadline is invalid":            "EXECUTION_DEADLINE",
		"workflow execution deadline scope is invalid":               "EXECUTION_DEADLINE",
		"runner STT configuration binding is invalid":                "STT_BINDING",
		"runner effective reasoning effort is invalid":               "REASONING_EFFORT",
		"runner config overlay binding is invalid":                   "CONFIG_OVERLAY",
		"runner environment policy binding is invalid":               "ENVIRONMENT_POLICY",
		"runner workspace policy binding is invalid":                 "WORKSPACE_POLICY",
		"runner file catalog binding is invalid":                     "FILE_CATALOG",
		"runner environment binding is invalid":                      "ENVIRONMENT_BINDING",
		"runner integration grant is outside effective capabilities": "INTEGRATION_CAPABILITIES",
		"runner project binding is invalid":                          "PROJECT_BINDING",
		"runner turn binding is invalid":                             "TURN_BINDING",
		"runner bounded input digest is invalid":                     "BOUNDED_INPUT",
		"runner execution binding digest is invalid":                 "EXECUTION_BINDING",
		"warm runner binding is invalid":                             "WARM_BINDING",
		"runner delegation catalog is invalid":                       "DELEGATION_CATALOG",
		"runner assistant context is invalid":                        "ASSISTANT_CONTEXT",
		"runner artifact catalog is invalid":                         "ATTACHMENT_CATALOG",
		"runner artifact provenance is invalid":                      "ATTACHMENT_CATALOG",
		"runner attachment set catalog is invalid":                   "ATTACHMENT_CATALOG",
		"runner attachment set binding is invalid":                   "ATTACHMENT_CATALOG",
		"runner attachment context is invalid":                       "ATTACHMENT_CATALOG",
	} {
		cause := errors.New(text)
		failure := &TurnInputBuildError{Stage: "validation", Cause: validationFailure("RUNNER_INPUT", cause)}
		if failure.ValidationReason() != want || !errors.Is(failure, cause) {
			t.Fatalf("fixed validator branch did not map to %s", want)
		}
	}
	if got := runnerInputValidationReason(fmt.Errorf("private: %w", runtimecontract.ErrRuntimeContext)); got != "CONTEXT_SNAPSHOT" {
		t.Fatal("exported context sentinel wrapping lost")
	}
	if runnerInputValidationReason(nil) != "UNKNOWN" || runnerInputValidationReason(errors.New("secret synthetic")) != "UNKNOWN" {
		t.Fatal("unknown reason leaked")
	}
}

func TestBuildTurnInputValidationDiagnosticPreservesGuard(t *testing.T) {
	manager := newTestManager(t, fake.NewSimpleClientset())
	for name, test := range map[string]struct {
		mutate func(*runtimecontract.RunnerInput)
		want   string
	}{
		"required context": {func(input *runtimecontract.RunnerInput) { input.ContextSnapshot = nil }, "REQUIRED_CONTEXT"},
		"envelope":         {func(input *runtimecontract.RunnerInput) { input.Schema = "synthetic-private" }, "ENVELOPE_SCHEMA"},
		"prompt": {func(input *runtimecontract.RunnerInput) {
			input.PromptServiceTemplateRevision = "prompt-service-unknown"
		}, "PROMPT_SERVICE"},
		"turn":          {func(input *runtimecontract.RunnerInput) { input.Task = "" }, "TURN_BINDING"},
		"bounded input": {func(input *runtimecontract.RunnerInput) { input.InputDigest = strings.Repeat("f", 64) }, "BOUNDED_INPUT"},
		"execution pin": {func(input *runtimecontract.RunnerInput) { input.ExecutionBindingDigest = strings.Repeat("f", 64) }, "EXECUTION_BINDING"},
	} {
		t.Run(name, func(t *testing.T) {
			input, _, err := manager.BuildTurnInput(testExecution(false))
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(&input)
			original := input.Validate()
			cause := validateRunnerInput(input)
			failure := &TurnInputBuildError{Stage: "validation", Cause: cause}
			if cause == nil || failure.ValidationReason() != test.want {
				t.Fatalf("validation reason = %s, want %s", failure.ValidationReason(), test.want)
			}
			if test.want != "REQUIRED_CONTEXT" && (original == nil || cause.Error() != original.Error()) {
				t.Fatal("existing validator rejection changed")
			}
		})
	}
}
