package platform

import (
	"encoding/json"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// scanSystemAssistant scans the fourteen-column assistant row and maps scan failures to an empty
// result and Unavailable. It retains the existing ignored limits-JSON errors, heartbeat-based
// READY/BUSY hint, system/nondeletable flags and role-specific action hints. These hints do not
// authorize execution or prove provider readiness. The caller retains its SQL, context, owner
// scope and transaction; this helper performs no query or authority resolution.
// scanSystemAssistant сканирует четырнадцать колонок помощника, при ошибке возвращает пустой
// результат и Unavailable. Сохраняет прежнее игнорирование ошибок JSON лимитов, подсказку READY/
// BUSY по heartbeat, system/nondeletable flags и подсказки действий роли. Эти признаки не дают
// полномочий исполнения и не доказывают готовность провайдера. SQL, контекст, область владельца
// и транзакция остаются у вызывающего; helper не делает запрос и не разрешает authority.
func scanSystemAssistant(row pgx.Row, role string) (entity.SystemAssistant, error) {
	var item entity.SystemAssistant
	var limits []byte
	err := row.Scan(&item.Ref, &item.StableKey, &item.Name, &item.Purpose, &item.CorePromptRevision, &item.OwnerInstructions, &item.RuntimeState, &item.RuntimeRevision, &item.DesiredRuntimeRevision, &item.WarmSessionRef, &limits, &item.LastHeartbeatAt, &item.Version, &item.UpdatedAt)
	if err != nil {
		return entity.SystemAssistant{}, errs.ErrUnavailable
	}
	_ = json.Unmarshal(limits, &item.ResourceLimits)
	item.Ready = contains([]string{"READY", "BUSY"}, item.RuntimeState) && item.LastHeartbeatAt != nil && time.Since(*item.LastHeartbeatAt) < 45*time.Second
	item.System = true
	item.Deletable = false
	item.NextActions = assistantActions(role, item.Ready)
	return item, nil
}
