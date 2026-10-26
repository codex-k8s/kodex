package oidcverifier

// SigningKeysInitialized reports whether this verifier has accepted its first
// complete signing-key snapshot. It performs no I/O and remains true after later
// refresh failures; callers must still verify each token with the current policy.
// SigningKeysInitialized сообщает, принят ли первый полный снимок ключей подписи.
// Метод не выполняет I/O и сохраняет true после последующих ошибок обновления;
// каждый токен по-прежнему проверяется по действующей политике.
func (verifier *Verifier) SigningKeysInitialized() bool {
	if verifier == nil || verifier.keys == nil {
		return false
	}
	verifier.keys.mutex.RLock()
	defer verifier.keys.mutex.RUnlock()
	return len(verifier.keys.keys.Keys) != 0
}
