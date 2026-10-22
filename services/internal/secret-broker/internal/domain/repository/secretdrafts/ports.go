package secretdrafts

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

var (
	// ErrNotFound identifies absence of the exact materialization, not permission to substitute another
	// object.
	// ErrNotFound обозначает отсутствие точной materialization, не разрешение подставить иной объект.
	ErrNotFound = errors.New("secret draft materialization is not found")
	// ErrConflict marks a mismatch with owner intent or pinned resource identity; callers must not accept the
	// observed replacement.
	// ErrConflict обозначает несовпадение с owner intent или закреплённой resource identity; caller не должен
	// принимать замену.
	ErrConflict = errors.New("secret draft materialization does not match owner intent")
	// ErrUnavailable reports a dependency/readiness failure without asserting whether an external effect
	// committed.
	// ErrUnavailable сообщает сбой dependency/readiness без утверждения о фиксации внешнего эффекта.
	ErrUnavailable = errors.New("secret draft dependency is unavailable")
	// ErrInvalid denotes malformed command input or binding, distinct from a missing object or owner-state
	// conflict.
	// ErrInvalid обозначает некорректный command input или binding, отдельно от отсутствия объекта и
	// owner-state conflict.
	ErrInvalid = errors.New("secret draft request is invalid")
)

// CleanupAckError wraps a cleanup-receipt failure with a diagnostic stage and inspectable cause; its Error
// text omits both.
// CleanupAckError оборачивает отказ cleanup receipt, сохраняя diagnostic stage и доступную cause; Error не
// выводит ни то ни другое.
type CleanupAckError struct {
	Stage string
	Cause error
}

// Error returns a fixed acknowledgement diagnostic without embedding stage, descriptors or the underlying
// cause.
// Error возвращает фиксированную acknowledgement diagnostic без stage, descriptors и текста исходной
// cause.
func (failure CleanupAckError) Error() string { return "secret draft cleanup acknowledgement failed" }

// Unwrap exposes the original cause for errors.Is/As without changing the public diagnostic.
// Unwrap предоставляет исходную cause для errors.Is/As без изменения публичной diagnostic.
func (failure CleanupAckError) Unwrap() error { return failure.Cause }

// Owner owns draft grants, claims, terminal metadata and recovery decisions; domain callers supply
// observations, not self-issued authority.
// Owner владеет draft grants, claims, terminal metadata и recovery decisions; domain callers передают
// observations, не самовыданную authority.
type Owner interface {
	// Check probes the authoritative draft-work route without claiming or executing work.
	// Check проверяет authoritative draft-work route без claim и выполнения работы.
	Check(context.Context) error
	// Consume exchanges an exact opaque grant for bound work; the service must still validate returned scope,
	// kind and lease before effects.
	// Consume обменивает точный opaque grant на связанную работу; service затем проверяет scope, kind и lease
	// до эффектов.
	Consume(context.Context, string) (value.DraftWork, error)
	// Complete records completion for the exact work and observed encrypted/runtime descriptors; a lost reply
	// does not authorize replaying effects.
	// Complete фиксирует завершение точной работы с observed encrypted/runtime descriptors; потерянный ответ
	// не разрешает повтор эффектов.
	Complete(context.Context, value.DraftWork, *value.DraftEncryptedDescriptor, *value.DraftMaterialization) (value.DraftResult, error)
	// Fail reports a definite command failure for the exact claim, not an ambiguous storage or completion
	// outcome.
	// Fail сообщает определённый command failure точного claim, не неоднозначный storage или completion
	// outcome.
	Fail(context.Context, value.DraftWork) error
	// ListRecovery returns bounded authoritative work for reconciliation without granting plaintext access or
	// permission to repeat publication.
	// ListRecovery возвращает ограниченную authoritative работу для reconciliation без plaintext access и
	// разрешения повторять публикацию.
	ListRecovery(context.Context) ([]value.DraftWork, error)
	// Recover decides KEEP/DELETE separately for observed ciphertext and runtime effects under the owner's
	// durable intent.
	// Recover раздельно определяет KEEP/DELETE observed ciphertext и runtime effects по устойчивому intent
	// владельца.
	Recover(context.Context, value.DraftWork, *value.DraftEncryptedDescriptor, *value.DraftMaterialization) (value.DraftRecoveryDecision, error)
	// CompleteCleanup acknowledges only exact descriptors selected for DELETE; retained publications must not
	// be included.
	// CompleteCleanup подтверждает лишь exact descriptors, выбранные для DELETE; сохраняемые публикации
	// включать нельзя.
	CompleteCleanup(context.Context, value.DraftWork, *value.DraftEncryptedDescriptor, *value.DraftMaterialization) error
}

// Cipher isolates authenticated encryption from lifecycle authority; bindings must be authenticated and
// returned sensitive buffers remain caller-owned.
// Cipher отделяет authenticated encryption от lifecycle authority; bindings должны аутентифицироваться,
// возвращённые чувствительные буферы принадлежат caller.
type Cipher interface {
	// Encrypt seals the supplied bytes for the exact draft binding and key identity; the caller must clear
	// returned ciphertext when no longer needed.
	// Encrypt шифрует переданные байты с точными draft binding и key identity; caller должен очищать
	// возвращённый ciphertext после использования.
	Encrypt(context.Context, value.SecretDraftBinding, []byte) (value.EncryptedSecretDraft, error)
	// Decrypt opens only ciphertext matching the authenticated binding and content digest; the caller must
	// clear returned plaintext even on later failure.
	// Decrypt открывает лишь ciphertext с совпавшими authenticated binding и content digest; caller очищает
	// plaintext и при последующем сбое.
	Decrypt(context.Context, value.SecretDraftBinding, value.EncryptedSecretDraft) ([]byte, error)
}

// EncryptedStore handles only scoped encrypted staging objects with exact descriptors; it neither
// publishes runtime values nor owns draft lifecycle.
// EncryptedStore работает только со scoped encrypted staging objects и exact descriptors; он не публикует
// runtime values и не владеет draft lifecycle.
type EncryptedStore interface {
	// Check probes encrypted staging prerequisites without creating a draft or reading its plaintext.
	// Check проверяет encrypted staging prerequisites без создания draft и чтения plaintext.
	Check(context.Context) error
	// Create stores the encrypted effect for exact owner work and returns its pinned descriptor; an uncertain
	// write outcome must remain recoverable.
	// Create сохраняет encrypted effect точной owner work и возвращает pinned descriptor; неопределённый write
	// outcome должен оставаться восстановимым.
	Create(context.Context, value.DraftWork, value.EncryptedSecretDraft) (value.DraftEncryptedDescriptor, error)
	// Read obtains ciphertext only after verifying the supplied work and exact object descriptor; returned
	// bytes are not model context.
	// Read получает ciphertext лишь после проверки work и exact object descriptor; возвращённые байты не
	// являются model context.
	Read(context.Context, value.DraftWork, value.DraftEncryptedDescriptor) (value.EncryptedSecretDraft, error)
	// Lookup observes the staging effect bound to work for recovery without decrypting it.
	// Lookup наблюдает staging effect, связанный с work, для recovery без расшифровки.
	Lookup(context.Context, value.DraftWork) (value.DraftEncryptedDescriptor, error)
	// Delete removes only the exact work-bound encrypted object with adapter-enforced identity preconditions.
	// Delete удаляет лишь точный связанный с work encrypted object с identity preconditions, проверяемыми
	// adapter.
	Delete(context.Context, value.DraftWork, value.DraftEncryptedDescriptor) error
}

// RuntimeStore owns publish/lookup/delete of work-bound runtime materializations, not grants or the
// decision to retain them.
// RuntimeStore выполняет publish/lookup/delete связанных с work runtime materializations, не выдаёт grants
// и не решает, сохранять ли их.
type RuntimeStore interface {
	// Publish materializes validated plaintext for an exact claimed PUBLISH work and returns identity pins;
	// uncertain success is resolved by recovery.
	// Publish материализует проверенный plaintext точной claimed PUBLISH work и возвращает identity pins;
	// неопределённый успех разрешает recovery.
	Publish(context.Context, value.DraftWork, []byte) (value.DraftMaterialization, error)
	// Lookup observes the exact publication for recovery without repeating Publish or returning plaintext.
	// Lookup наблюдает точную публикацию для recovery без повторного Publish и возврата plaintext.
	Lookup(context.Context, value.DraftWork) (value.DraftMaterialization, error)
	// Delete removes only the observed materialization matching the supplied work and exact object pins.
	// Delete удаляет лишь observed materialization с совпавшими work и exact object pins.
	Delete(context.Context, value.DraftWork, value.DraftMaterialization) error
}

// Checker is the narrow readiness port for key prerequisites, not a grant issuer or encryption operation.
// Checker — узкий readiness port prerequisites ключей, не источник grants и не операция шифрования.
type Checker interface {
	// Check reports current key readiness without consuming a command grant or an encryption allowance.
	// Check сообщает текущую готовность ключей без расхода command grant и разрешённого шифрования.
	Check(context.Context) error
}

// Observer receives completed local cleanup effects and overall recovery outcome; it neither acknowledges
// owner receipts nor authorizes deletion.
// Observer получает сведения о выполненных local cleanup effects и общем recovery outcome; он не
// подтверждает owner receipts и не разрешает удаление.
type Observer interface {
	// EncryptedDeleted records a successful ciphertext deletion even if a later cleanup acknowledgement fails.
	// EncryptedDeleted учитывает успешное удаление ciphertext, даже если последующий cleanup acknowledgement
	// завершится ошибкой.
	EncryptedDeleted()
	// RuntimeDeleted records a successful runtime deletion independently of later reconciliation failure.
	// RuntimeDeleted учитывает успешное runtime deletion независимо от последующего reconciliation failure.
	RuntimeDeleted()
	// RecoveryCompleted observes one reconciliation result; false does not erase already completed cleanup
	// effects.
	// RecoveryCompleted наблюдает результат одного reconciliation; false не отменяет уже выполненные cleanup
	// effects.
	RecoveryCompleted(bool)
}
