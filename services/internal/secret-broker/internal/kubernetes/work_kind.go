package kubernetes

// MaterializationWorkKind фиксирует единственный owner route внешнего эффекта.
// Значение назначается серверным adapter после проверки owner claim, не caller.
type MaterializationWorkKind string

const (
	WorkKindImmediate MaterializationWorkKind = "IMMEDIATE"
	WorkKindDraft     MaterializationWorkKind = "DRAFT"
)

func (kind MaterializationWorkKind) Valid() bool {
	return kind == WorkKindImmediate || kind == WorkKindDraft
}
