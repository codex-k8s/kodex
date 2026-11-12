package errs

// unavailableCause retains a private dependency error while preserving the service's stable unavailable identity.
// unavailableCause сохраняет закрытую ошибку зависимости вместе с устойчивой идентичностью недоступности сервиса.
type unavailableCause struct{ cause error }

// Error returns only the domain sentinel text; formatting never invokes the dependency error.
// Error возвращает только текст доменного sentinel; форматирование никогда не вызывает ошибку зависимости.
func (*unavailableCause) Error() string { return Unavailable.Error() }

// Is preserves existing transport/domain unavailable matching without discarding the underlying typed cause.
// Is сохраняет прежнюю проверку недоступности в transport/domain без потери типизированной причины.
func (*unavailableCause) Is(target error) bool { return target == Unavailable }

// Unwrap permits private errors.Is/As inspection without granting permission to serialize the cause.
// Unwrap допускает закрытую проверку errors.Is/As, не разрешая сериализацию причины.
func (e *unavailableCause) Unwrap() error { return e.cause }

// WrapUnavailable preserves a dependency cause for local diagnostics and returns the canonical sentinel when no cause exists.
// WrapUnavailable сохраняет причину отказа зависимости для локальной диагностики и возвращает канонический sentinel при её отсутствии.
func WrapUnavailable(cause error) error {
	if cause == nil {
		return Unavailable
	}
	return &unavailableCause{cause: cause}
}
