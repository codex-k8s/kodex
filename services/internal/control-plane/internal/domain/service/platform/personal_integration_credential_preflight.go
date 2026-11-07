package platform

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// IntegrationCredentialPreflightError retains the full safe problem list while classifying launch
// as conflict. It contains no secret descriptor, value or user-selected identity.
// IntegrationCredentialPreflightError сохраняет полный безопасный список проблем и классифицирует
// запуск как conflict. Secret descriptor, значение и выбранная пользователем identity отсутствуют.
type IntegrationCredentialPreflightError struct {
	Issues []entity.IntegrationCredentialPreflightIssue
}

// Error exposes one fixed diagnostic instead of serializing the owner problem list into logs.
// Error возвращает одну фиксированную диагностику вместо сериализации owner problem list в logs.
func (*IntegrationCredentialPreflightError) Error() string {
	return "required personal integration credentials are not ready"
}

// Unwrap preserves the existing closed conflict mapping until a typed transport projection is used.
// Unwrap сохраняет существующее закрытое conflict mapping для типизированной transport-проекции.
func (*IntegrationCredentialPreflightError) Unwrap() error { return errs.ErrConflict }
