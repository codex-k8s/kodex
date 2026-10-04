package platform

import (
	"errors"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

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
