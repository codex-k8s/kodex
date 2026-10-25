package grpc

import (
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// castTemplateVariable validates the closed availability reason before allocating a wire
// variable, then copies schema/example metadata and item fields in their original order.
// Invalid reasons return a raw error and no partial variable; nil or empty item fields stay
// nil. The caller owns response assembly and transport error mapping. This projection neither
// resolves a variable value nor establishes authority over its source or context.
// castTemplateVariable проверяет закрытую причину доступности до выделения wire-переменной,
// затем копирует метаданные схемы, примеров и поля элемента в исходном порядке. Неверная причина
// возвращает исходную ошибку без частичной переменной; nil либо пустые поля элемента остаются
// nil. Сборка ответа и transport error mapping принадлежат вызывающему методу. Проекция не
// разрешает значение переменной и не назначает полномочия на её источник либо контекст.
func castTemplateVariable(value entity.TemplateVariable) (*controlplanev1.TemplateVariable, error) {
	reason, err := castTemplateAvailabilityReason(value.Reason, value.Available)
	if err != nil {
		return nil, err
	}
	variable := &controlplanev1.TemplateVariable{Name: value.Name, ValueType: value.Type, Description: value.Description,
		Example: value.Example, Source: value.Source, Collection: value.Collection, ItemValueType: value.ItemType,
		RangeExample: value.RangeExample, Available: value.Available, Reason: reason}
	for _, field := range value.ItemFields {
		variable.ItemFields = append(variable.ItemFields, &controlplanev1.TemplateVariableField{Name: field.Name, ValueType: field.Type, Description: field.Description})
	}
	return variable, nil
}
