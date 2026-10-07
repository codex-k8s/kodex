package callback

import (
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	delegationInputInvalidCode     = "DELEGATION_INPUT_INVALID"
	delegationInputInvalidGuidance = "Read the delegate_agent input schema and copy one exact current server-owned target_agent_ref/workflow_step_key pair. Correct only the rejected input and retry at most once. Do not retry accepted delegation, RPC failures or unknown outcomes."
)

// Маркер создаётся только до owner RPC; remote status и outcome его не получают.
type delegationInputError struct{ reason string }

func (*delegationInputError) Error() string { return "delegation input is invalid" }

func delegationInputFailureClass(err error) string {
	var inputErr *delegationInputError
	if !errors.As(err, &inputErr) {
		return ""
	}
	switch inputErr.reason {
	case "shape", "selection", "task", "input":
		return "delegation_input_" + inputErr.reason
	default:
		return ""
	}
}

func validateDelegationInput(input runtimecontract.RunnerInput, arguments map[string]any) (string, string, string, *structpb.Struct, error) {
	reject := func(reason string) (string, string, string, *structpb.Struct, error) {
		return "", "", "", nil, &delegationInputError{reason: reason}
	}
	if !onlyKeys(arguments, "target_agent_ref", "workflow_step_key", "task", "input") {
		return reject("shape")
	}
	target, targetOK := arguments["target_agent_ref"].(string)
	stepKey, stepOK := arguments["workflow_step_key"].(string)
	if _, present := arguments["workflow_step_key"]; present && !stepOK {
		return reject("shape")
	}
	if !targetOK {
		return reject("shape")
	}
	allowed := false
	for _, item := range input.DelegationTargets {
		if item.Ref == target && item.WorkflowStepKey == stepKey {
			allowed = true
			break
		}
	}
	if !allowed {
		return reject("selection")
	}
	task, taskOK := arguments["task"].(string)
	if !taskOK || strings.TrimSpace(task) == "" || len(task) > 64<<10 {
		return reject("task")
	}
	var bounded map[string]any
	if raw, present := arguments["input"]; present {
		var valid bool
		bounded, valid = raw.(map[string]any)
		if !valid {
			return reject("input")
		}
	}
	structure, err := structpb.NewStruct(bounded)
	if err != nil {
		return reject("input")
	}
	return target, stepKey, task, structure, nil
}
