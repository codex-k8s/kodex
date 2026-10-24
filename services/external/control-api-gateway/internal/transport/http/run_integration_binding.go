package httptransport

import (
	"errors"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// validateRunIntegrationBinding validates an optional RunEvent invocation ref, its inv_ prefix and
// TURN_PROGRESS/canonical outcome combination. Other message descriptors and events without the
// field are unchanged; the ref itself grants no execution authority.
// validateRunIntegrationBinding проверяет необязательный invocation ref в RunEvent, префикс inv_ и
// сочетание TURN_PROGRESS с каноническим исходом. Другие сообщения и события без поля не меняются;
// сам ref не предоставляет полномочий исполнения.
func validateRunIntegrationBinding(value map[string]any, descriptor protoreflect.MessageDescriptor) error {
	if descriptor.FullName() != "controlplane.v1.RunEvent" {
		return nil
	}
	raw, present := value["integrationInvocationRef"]
	if !present {
		return nil
	}
	ref, ok := raw.(string)
	if !ok || len(ref) > 96 || !strings.HasPrefix(ref, "inv_") || !fileTargetRef(ref) || value["type"] != "RUN_EVENT_TYPE_TURN_PROGRESS" {
		return errors.New("run integration binding is invalid")
	}
	switch value["summary"] {
	case "i18n:INTEGRATION_ACTION_SUCCEEDED", "i18n:INTEGRATION_ACTION_FAILED", "i18n:INTEGRATION_ACTION_OUTCOME_UNKNOWN":
		return nil
	default:
		return errors.New("run integration binding outcome is invalid")
	}
}
