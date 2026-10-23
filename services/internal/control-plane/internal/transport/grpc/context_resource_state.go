package grpc

import (
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

// domainContextResourceState maps the shared wire enum to the domain filter, preserving an empty filter for UNSPECIFIED and rejecting unknown enum numbers.
// domainContextResourceState переводит общий транспортный enum в доменный фильтр, сохраняя пустой фильтр для UNSPECIFIED и отклоняя неизвестные числовые значения.
func domainContextResourceState(state controlplanev1.ContextResourceState) (string, bool) {
	if state == controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_UNSPECIFIED {
		return "", true
	}
	name, ok := controlplanev1.ContextResourceState_name[int32(state)]
	if !ok {
		return "", false
	}
	return strings.TrimPrefix(name, "CONTEXT_RESOURCE_STATE_"), true
}
