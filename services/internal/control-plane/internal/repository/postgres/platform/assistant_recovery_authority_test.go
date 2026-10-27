package platform

import (
	"errors"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

// TestAssistantRecoveryHasExplicitOrganizationAuthority checks RecoverAssistant selects
// organization.manage with exact organization target, rejects project-bound authority and requires
// the typed empty struct payload. A nil transaction is intentional because this routing branch
// performs no DB lookup; permission evaluation is not exercised.
// TestAssistantRecoveryHasExplicitOrganizationAuthority проверяет выбор organization.manage и
// exact organization target для RecoverAssistant, отказ project-bound authority и требование typed
// empty struct payload. Nil transaction намеренен: routing branch не читает БД; permission
// evaluation не выполняется.
func TestAssistantRecoveryHasExplicitOrganizationAuthority(t *testing.T) {
	repository := &Repository{}
	current := scope{organizationRef: "org_recovery_fixture"}
	input := command.Command{Kind: command.RecoverAssistant, Payload: struct{}{}}
	permission, target, err := repository.commandAccessTarget(t.Context(), nil, current, input)
	if err != nil || permission != "organization.manage" || !reflect.DeepEqual(target.scope, organizationTarget(current.organizationRef)) {
		t.Fatalf("exact recovery authority: permission=%s err=%v", permission, err)
	}
	current.authorityProjectID = "project-bound-worker"
	if _, _, err := repository.commandAccessTarget(t.Context(), nil, current, input); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("project worker recovered global assistant: %v", err)
	}
	current.authorityProjectID = ""
	input.Payload = nil
	if _, _, err := repository.commandAccessTarget(t.Context(), nil, current, input); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("untyped recovery payload: %v", err)
	}
}
