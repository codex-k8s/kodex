package callback

import (
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	// delegationInputInvalidCode identifies only correctable local delegation input
	// rejections; remote owner/audit failures must not acquire this recovery authority.
	// delegationInputInvalidCode обозначает лишь исправимые локальные delegation input
	// отказы; remote owner/audit failures не получают эту recovery authority.
	delegationInputInvalidCode = "DELEGATION_INPUT_INVALID"
	// delegationInputInvalidGuidance asks for one corrected exact target/step pair
	// and forbids replaying accepted delegations or uncertain owner outcomes.
	// delegationInputInvalidGuidance требует одного исправления exact target/step pair
	// и запрещает повтор принятого delegation или uncertain owner outcome.
	delegationInputInvalidGuidance = "Read the delegate_agent input schema and copy one exact current server-owned target_agent_ref/workflow_step_key pair. Correct only the rejected input and retry at most once. Do not retry accepted delegation, RPC failures or unknown outcomes."
)

// delegationInputError carries a closed local pre-owner validation reason without
// caller data. Only locally created errors should use this marker, not remote status.
// delegationInputError хранит closed local pre-owner validation reason без caller
// data. Marker назначается лишь локальным отказам, не remote status.
type delegationInputError struct{ reason string }

// Error returns fixed safe text independent of the local reason, target or task.
// Error возвращает fixed safe text независимо от local reason,target и task.
func (*delegationInputError) Error() string { return "delegation input is invalid" }

// delegationInputFailureClass recognizes the typed error through wrapping but
// emits only four registered reason labels; text matches or unknown reasons give no class.
// delegationInputFailureClass распознаёт typed error через wrapping, но выдаёт лишь
// четыре registered reason labels; совпадение текста и unknown reason не дают class.
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

// validateDelegationInput accepts a closed shape and one exact snapshot target/step
// pair, requires nonblank task within 64 KiB and converts optional object input to protobuf.
// It preserves original task text, distinguishes absent input from explicit null and
// does not resolve current owner permissions or perform the delegation.
// validateDelegationInput допускает closed shape и exact snapshot target/step pair,
// требует nonblank task до 64 KiB и переводит optional object input в protobuf.
// Исходный task сохраняется, absent input отличается от explicit null; текущие owner
// permissions не разрешаются и delegation не исполняется.
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
