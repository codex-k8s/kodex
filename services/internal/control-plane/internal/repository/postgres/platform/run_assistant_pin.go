package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/run_assistant_pin.sql
var queryRunAssistantPin string

// Readback назначает pin только из сохранённой owner identity. Он не расширяет
// eligibility исходного чтения Run и не превращается в authority для команды.
func attachRunAssistantPin(ctx context.Context, runner queryRunner, current scope, run *entity.Run) error {
	if run.Target.Type != "SYSTEM_ASSISTANT" {
		if run.ProjectRef == "" {
			return errs.ErrUnavailable
		}
		return nil
	}
	if run.Source != "SYSTEM_ASSISTANT" {
		return errs.ErrUnavailable
	}
	var pin entity.AssistantRunPin
	err := runner.QueryRow(ctx, queryRunAssistantPin, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "run_ref": run.Ref,
	}).Scan(&pin.Scope, &pin.OrganizationRef, &pin.ConversationRef, &pin.AssistantRef, &pin.ProjectRef, &pin.ProfileRef)
	if err != nil || !validRunAssistantPin(*run, pin) {
		return errs.ErrUnavailable
	}
	run.AssistantPin = &pin
	return nil
}

func validRunAssistantPin(run entity.Run, pin entity.AssistantRunPin) bool {
	if pin.OrganizationRef == "" || pin.ConversationRef == "" || pin.AssistantRef == "" ||
		pin.AssistantRef != run.Target.Ref || pin.ProjectRef != run.ProjectRef {
		return false
	}
	switch pin.Scope {
	case "SYSTEM":
		return pin.ProfileRef == ""
	case "PROJECT":
		return pin.ProjectRef != "" && pin.ProfileRef != ""
	default:
		return false
	}
}
