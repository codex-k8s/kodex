// Package objectstorage defines the shared port and metadata types for versioned object content; callers own key and tenant authorization.
// Пакет objectstorage задаёт общий порт и типы метаданных для версионированного содержимого объектов; ключи и проверка полномочий арендатора принадлежат вызывающей стороне.
package objectstorage

import (
	"context"
	"errors"
	"io"
)

var (
	// ErrInvalid reports a syntactically invalid key, digest, body, or required request metadata before storage access.
	// ErrInvalid обозначает неверный синтаксис ключа, дайджеста, тела или обязательных метаданных запроса до обращения к хранилищу.
	ErrInvalid = errors.New("object storage input is invalid")
	// ErrNotFound reports that the selected object or requested version is absent.
	// ErrNotFound обозначает отсутствие выбранного объекта или запрошенной версии.
	ErrNotFound = errors.New("object storage object is not found")
	// ErrConflict reports a mismatch between requested and observed object metadata, including an invalid stored digest.
	// ErrConflict обозначает расхождение запрошенных и наблюдаемых метаданных объекта, включая неверный сохранённый дайджест.
	ErrConflict = errors.New("object storage object metadata conflicts with the request")
	// ErrUnavailable reports a storage service or transport failure without claiming the intended write did not occur.
	// ErrUnavailable обозначает отказ сервиса хранилища или транспорта, не утверждая, что задуманная запись не состоялась.
	ErrUnavailable = errors.New("object storage is unavailable")
)

// PutInput carries a caller-assigned key, declared content metadata, and a streaming body for one object write.
// PutInput передаёт назначенный вызывающей стороной ключ, заявленные метаданные и поток тела для одной записи объекта.
// The port does not itself prove the body matches Digest or that the key belongs to the caller's tenant.
// Сам порт не доказывает соответствие тела полю Digest или принадлежность ключа арендатору вызывающей стороны.
type PutInput struct {
	Key, MediaType, Digest string
	SizeBytes              int64
	Body                   io.Reader
}

// Receipt records the observed key, provider version, ETag, digest, and size returned after a storage operation.
// Receipt сохраняет наблюдаемые ключ, версию провайдера, ETag, дайджест и размер после операции с хранилищем.
type Receipt struct {
	Key, VersionID, ETag, Digest string
	SizeBytes                    int64
}

// Object combines a receipt with a readable body; callers must close the body after retrieval.
// Object объединяет квитанцию с потоком тела; после получения вызывающая сторона обязана закрыть поток.
type Object struct {
	Receipt
	Body io.ReadCloser
}

// Store hides provider endpoints and credentials from the domain; it does not decide object ownership or mint access grants.
// Store скрывает адреса провайдера и учётные данные от домена; он не определяет владение объектом и не выдаёт право доступа.
// The caller must provide a server-assigned key containing its tenant boundary and pin a version when exact readback is required.
// Вызывающая сторона должна передать назначенный сервером ключ с областью арендатора и закрепить версию, когда требуется точное повторное чтение.
type Store interface {
	// Check probes adapter availability but does not authorize a particular object operation.
	// Check проверяет доступность адаптера, но не разрешает конкретную операцию над объектом.
	Check(context.Context) error
	// Put submits one body with declared metadata and returns a provider receipt; unknown outcomes require owner-side reconciliation.
	// Put отправляет одно тело с заявленными метаданными и возвращает квитанцию провайдера; неопределённый исход требует сверки владельцем состояния.
	Put(context.Context, PutInput) (Receipt, error)
	// Get retrieves the selected key and optional version with a body that the caller must close.
	// Get получает выбранный ключ и необязательную версию с телом, поток которого должна закрыть вызывающая сторона.
	Get(context.Context, string, string) (Object, error)
	// Head reads receipt metadata for the selected key and optional version without returning a body.
	// Head читает метаданные квитанции для выбранного ключа и необязательной версии без возврата тела.
	Head(context.Context, string, string) (Receipt, error)
	// Delete requests removal of the selected key or version; owner lifecycle decides when deletion is permitted.
	// Delete запрашивает удаление выбранного ключа или версии; допустимость удаления определяется жизненным циклом владельца.
	Delete(context.Context, string, string) error
}
