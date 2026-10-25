package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryRunAssistantPin embeds exact run/session/conversation/assistant/profile lineage within the
// organization, distinguishing SYSTEM and PROJECT assistant ownership. It assigns readback pins
// from owner rows after canonical run eligibility, not from a client descriptor.
// queryRunAssistantPin встраивает точную связь run/session/conversation/assistant/profile внутри
// организации, различая SYSTEM и PROJECT владение ассистента. Назначает readback pins из строк
// владельца после канонической проверки видимости запуска, а не из дескриптора клиента.
//
//go:embed sql/run_assistant_pin.sql
var queryRunAssistantPin string

// attachRunAssistantPin requires a project for nonassistant runs and matching SYSTEM_ASSISTANT
// source for assistant targets. It reads and structurally validates the exact owner pin before
// attaching it; any corrupt/missing assistant data is Unavailable. It does not broaden run
// visibility or authorize a command.
// attachRunAssistantPin требует проект обычного запуска и источник SYSTEM_ASSISTANT для цели
// ассистента. Читает и структурно проверяет точный pin владельца перед добавлением; повреждённые
// либо отсутствующие данные ассистента дают Unavailable. Не расширяет видимость запуска и не
// разрешает команду.
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

// validRunAssistantPin requires nonempty organization/conversation/assistant identity and exact
// target/project equality. SYSTEM forbids a profile ref; PROJECT requires both project and
// profile. It checks supplied tuple consistency only, not current access or database lineage.
// validRunAssistantPin требует непустые организацию, разговор и ассистента и точное совпадение
// цели и проекта. SYSTEM запрещает ссылку профиля; PROJECT требует проект и профиль. Проверяет
// только согласованность переданного кортежа, а не текущий доступ и связи БД.
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
