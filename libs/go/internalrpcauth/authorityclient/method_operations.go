package authorityclient

// MethodOperations is a caller-owned exact full-method to operation-ID map for
// OperationResolver. It performs no registration validation or authorization;
// callers choose their closed registry and synchronize any later mutation.
// MethodOperations — принадлежащая вызывающему карта точного full-method к
// operation-ID для OperationResolver. Registration validation и авторизация
// не выполняются; закрытый registry и синхронизацию изменений задают callers.
type MethodOperations map[string]string

// OperationID returns the map value and presence flag unchanged, without
// normalization, fallback or copying. Nil maps report absence; a present empty
// value remains present and must be rejected by caller policy when required.
// OperationID возвращает map value и presence flag без изменения, нормализации,
// fallback и копирования. Nil maps означают отсутствие; присутствующее пустое
// значение остаётся присутствующим и при необходимости отклоняется caller policy.
func (operations MethodOperations) OperationID(fullMethod string) (string, bool) {
	value, ok := operations[fullMethod]
	return value, ok
}
