package app

import (
	"context"
	"log/slog"
)

const (
	// oidcRefreshClassKey names the closed key-refresh diagnostic without adding a metric label.
	// RU: oidcRefreshClassKey задаёт имя закрытого класса обновления ключей без новой метки метрики.
	oidcRefreshClassKey = "error_class"
	// oidcInitialDegradedMessage identifies the first fetch already performed by verifier construction.
	// RU: oidcInitialDegradedMessage обозначает первое получение, уже выполненное конструктором verifier.
	oidcInitialDegradedMessage = "OIDC initial signing-key refresh degraded"
	// oidcRefreshDegradedMessage marks a later transition into degraded key refresh.
	// RU: oidcRefreshDegradedMessage отмечает последующий переход обновления ключей в деградацию.
	oidcRefreshDegradedMessage = "OIDC signing-key refresh degraded"
	// oidcRefreshRestoredMessage marks recovery without exposing a key or issuer identifier.
	// RU: oidcRefreshRestoredMessage отмечает восстановление без раскрытия ключа или идентификатора issuer.
	oidcRefreshRestoredMessage = "OIDC signing-key refresh restored"
)

// signingKeyRefresher binds the existing refresh worker to the verifier's closed diagnostic snapshot; it introduces no readiness operation.
// RU: signingKeyRefresher связывает существующий worker с закрытым диагностическим снимком verifier без новой операции readiness.
type signingKeyRefresher interface {
	// Refresh performs the existing bounded key fetch without refreshing browser sessions.
	// RU: Refresh выполняет прежнее ограниченное получение ключей без обновления браузерных сессий.
	Refresh(context.Context) error
	// SigningKeyFailureClass returns a closed failure class or an empty string after a successful refresh.
	// RU: SigningKeyFailureClass возвращает закрытый класс отказа либо пустую строку после успешного обновления.
	SigningKeyFailureClass() string
}

// recordInitialOIDCRefresh reports the already-observed initial failure once without fetching keys again or changing startup policy.
// RU: recordInitialOIDCRefresh однократно сообщает уже наблюдавшийся начальный отказ без повторного получения ключей и изменения startup policy.
func recordInitialOIDCRefresh(ctx context.Context, logger *slog.Logger, verifier signingKeyRefresher) {
	if class := verifier.SigningKeyFailureClass(); class != "" {
		logger.WarnContext(ctx, oidcInitialDegradedMessage, oidcRefreshClassKey, class)
	}
}
