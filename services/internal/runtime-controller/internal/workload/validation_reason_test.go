package workload

import (
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
		"envelope":         {func(input *runtimecontract.RunnerInput) { input.Schema = "synthetic-private" }, "ENVELOPE"},
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
