package platform

import (
	"reflect"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// finishRehydratedAssistantOperation propagates hydration errors, rejects snapshot drift,
// and preserves the edited selection. It does not validate authority or write state.
// finishRehydratedAssistantOperation передаёт ошибку подготовки, отклоняет изменение снимка
// и сохраняет выбор правки. Не проверяет полномочия и не записывает состояние.
func finishRehydratedAssistantOperation(before map[string]any, selected bool, hydrated entity.AssistantPlanOperation, err error) (entity.AssistantPlanOperation, error) {
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if !reflect.DeepEqual(hydrated.Before, before) {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	hydrated.Selected = selected
	return hydrated, nil
}
