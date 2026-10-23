package kubernetes

// MaterializationWorkKind records the explicit owner route selected by the server adapter after claim
// validation, never inferred from a caller's operation reference.
// MaterializationWorkKind фиксирует явный маршрут владельца, выбранный серверным адаптером после
// проверки claim, а не выведенный из ссылки операции вызывающего.
type MaterializationWorkKind string

const (
	// WorkKindImmediate routes the external effect to the immediate secret-operation owner and its recovery
	// path.
	// WorkKindImmediate направляет внешний эффект владельцу immediate secret-operation и его recovery-пути.
	WorkKindImmediate MaterializationWorkKind = "IMMEDIATE"
	// WorkKindDraft routes the external effect to the secret-draft owner instead of treating a draft
	// reference as an immediate operation.
	// WorkKindDraft направляет внешний эффект владельцу secret-draft, не трактуя ссылку draft как immediate
	// операцию.
	WorkKindDraft MaterializationWorkKind = "DRAFT"
)

// Valid accepts only the two explicit materialization owner routes and rejects empty or future unknown
// values without upgrading them.
// Valid допускает только два явных маршрута владельца materialization и отклоняет пустые или будущие
// неизвестные значения без их преобразования.
func (kind MaterializationWorkKind) Valid() bool {
	return kind == WorkKindImmediate || kind == WorkKindDraft
}
