package callback

import (
	"errors"
	"strconv"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// assistantPlanInvalidDiagnostic stores only registered plan-stage/field labels and
// one-based operation index, excluding raw rejected parameters or owner credentials.
// assistantPlanInvalidDiagnostic хранит лишь registered plan-stage/field labels и one-
// based operation index без raw rejected parameters и owner credentials.
type assistantPlanInvalidDiagnostic struct {
	stage string
	field string
	index int
}

// assistantPlanInvalidDetails accepts a validated attached local diagnostic or one
// exact InvalidArgument ErrorInfo from the internal owner domain, with closed reason/
// metadata keys and canonical bounded index. Raw message text, duplicate details,
// unknown proto data and foreign fields cannot supply diagnostic authority.
// assistantPlanInvalidDetails принимает validated attached local diagnostic либо один
// точный InvalidArgument ErrorInfo internal owner domain с closed reason/metadata keys
// и canonical bounded index. Raw message text,duplicate details,unknown proto data
// и foreign fields не создают diagnostic authority.
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

// validAssistantPlanInvalidDiagnostic accepts five registered stages and indices
// one through32, with an optional closed field label; it resolves no plan or permission.
// validAssistantPlanInvalidDiagnostic допускает пять registered stages и indices один–32
// с optional closed field label,не разрешая plan и permission.
func validAssistantPlanInvalidDiagnostic(value assistantPlanInvalidDiagnostic) bool {
	switch value.stage {
	case "hydrate", "normalize", "command", "bind", "authorize":
		return value.index >= 1 && value.index <= 32 && (value.field == "" || validAssistantPlanInvalidField(value.field))
	default:
		return false
	}
}

// validAssistantPlanInvalidField allows only eighteen registered workflow-related
// failure labels, never arbitrary JSON paths or reflected model/user input.
// validAssistantPlanInvalidField допускает лишь восемнадцать registered workflow-related
// failure labels,не arbitrary JSON paths и не reflected model/user input.
func validAssistantPlanInvalidField(field string) bool {
	switch field {
	case "WORKFLOW_SHAPE", "WORKFLOW_REF", "WORKFLOW_VERSION", "WORKFLOW_BINDING",
		"MAX_CONCURRENCY", "TIMEOUT_SECONDS", "WORKFLOW_TEXT", "INPUT_FIELDS", "STEPS",
		"INPUT_FIELD_KEY", "STEP_SHAPE", "STEP_KEY", "STEPS_GRAPH", "WORKFLOW_DRAFT",
		"STEPS_INSTRUCTIONS", "STEPS_EXPECTED_RESULT", "WORKFLOW_INVARIANTS", "WORKFLOW_TEXT_BUDGET":
		return true
	default:
		return false
	}
}
