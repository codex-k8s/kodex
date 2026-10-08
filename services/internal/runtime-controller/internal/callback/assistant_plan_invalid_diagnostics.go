package callback

import (
	"errors"
	"strconv"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type assistantPlanInvalidDiagnostic struct {
	stage string
	field string
	index int
}

// Только typed detail точного внутреннего RPC, не payload или текст ошибки.
func assistantPlanInvalidDetails(err error) (assistantPlanInvalidDiagnostic, bool) {
	var inputErr *assistantPlanInputError
	if errors.As(err, &inputErr) && inputErr.diagnostic != nil {
		value := *inputErr.diagnostic
		return value, validAssistantPlanInvalidDiagnostic(value)
	}
	value := status.Convert(err)
	if value.Code() != codes.InvalidArgument || len(value.Details()) != 1 {
		return assistantPlanInvalidDiagnostic{}, false
	}
	info, ok := value.Details()[0].(*errdetails.ErrorInfo)
	if !ok || info.Domain != "kodex.control-plane" || len(info.ProtoReflect().GetUnknown()) != 0 || info.Metadata["category"] != "INVALID" {
		return assistantPlanInvalidDiagnostic{}, false
	}
	stage := ""
	switch info.Reason {
	case "ASSISTANT_PLAN_HYDRATE":
		stage = "hydrate"
	case "ASSISTANT_PLAN_NORMALIZE":
		stage = "normalize"
	case "ASSISTANT_PLAN_COMMAND":
		stage = "command"
	case "ASSISTANT_PLAN_BIND":
		stage = "bind"
	case "ASSISTANT_PLAN_AUTHORIZE":
		stage = "authorize"
	default:
		return assistantPlanInvalidDiagnostic{}, false
	}
	index, errIndex := strconv.Atoi(info.Metadata["operation_index"])
	field, hasField := info.Metadata["field"]
	expectedKeys := 2
	if hasField {
		expectedKeys++
		if !validAssistantPlanInvalidField(field) {
			return assistantPlanInvalidDiagnostic{}, false
		}
	}
	result := assistantPlanInvalidDiagnostic{stage: stage, field: field, index: index}
	return result, errIndex == nil && strconv.Itoa(index) == info.Metadata["operation_index"] && len(info.Metadata) == expectedKeys && validAssistantPlanInvalidDiagnostic(result)
}

func validAssistantPlanInvalidDiagnostic(value assistantPlanInvalidDiagnostic) bool {
	switch value.stage {
	case "hydrate", "normalize", "command", "bind", "authorize":
		return value.index >= 1 && value.index <= 32 && (value.field == "" || validAssistantPlanInvalidField(value.field))
	default:
		return false
	}
}

func validAssistantPlanInvalidField(field string) bool {
	switch field {
	case "WORKFLOW_SHAPE", "WORKFLOW_REF", "WORKFLOW_VERSION", "WORKFLOW_BINDING",
		"MAX_CONCURRENCY", "TIMEOUT_SECONDS", "WORKFLOW_TEXT", "INPUT_FIELDS", "STEPS",
		"INPUT_FIELD_KEY", "STEP_SHAPE", "STEP_KEY", "STEPS_GRAPH", "WORKFLOW_DRAFT",
		"STEPS_INSTRUCTIONS", "STEPS_EXPECTED_RESULT", "WORKFLOW_INVARIANTS":
		return true
	default:
		return false
	}
}
