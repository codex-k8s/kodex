package value

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
)

// DraftOperation names an owner-issued draft command; the string type alone does not reject
// unknown values or authorize an operation without verified work and a current claim.
// DraftOperation обозначает выданную владельцем draft command; строковый тип сам не отвергает
// неизвестные значения и не разрешает операцию без проверенного work и текущего claim.
type DraftOperation string

const (
	// DraftSave requests encrypted staging of the supplied value, not active runtime publication.
	// DraftSave запрашивает encrypted staging переданного значения, не active runtime publication.
	DraftSave DraftOperation = "SAVE"
	// DraftValidate requests validation of the staged revision without publishing runtime plaintext.
	// DraftValidate запрашивает проверку staged revision без публикации runtime plaintext.
	DraftValidate DraftOperation = "VALIDATE"
	// DraftPublish requests authorized runtime materialization and owner completion of the current draft.
	// DraftPublish запрашивает разрешённую runtime materialization и owner completion текущего draft.
	DraftPublish DraftOperation = "PUBLISH"
	// DraftDiscard requests disposal of draft staging while leaving an already published secret independent.
	// DraftDiscard запрашивает удаление draft staging независимо от уже опубликованного secret.
	DraftDiscard DraftOperation = "DISCARD"
)

// SecretDraft carries scoped owner metadata, versions, state and timestamps but no secret value.
// Its exported fields are not self-validating or an authority source; adapters/services verify them.
// SecretDraft переносит scoped owner metadata, версии, state и timestamps без secret value.
// Публичные поля сами не проверяются и не дают authority; их проверяют adapters/services.
type SecretDraft struct {
	ScopeKind                                                       runtimesecret.ScopeKind
	OrganizationRef                                                 string
	Ref, ProjectRef, SecretRef, Name, Description, ValueType, State string
	Version, Generation, PublishedRevision, SecretVersion           int64
	CreatedAt, UpdatedAt, ExpiresAt                                 time.Time
}

// DraftEncryptedDescriptor identifies an exact staged object by namespace/name/key, UID/version,
// ciphertext digest and encryption-key identity; it contains neither plaintext nor key material.
// DraftEncryptedDescriptor обозначает точный staged object через namespace/name/key, UID/version,
// ciphertext digest и encryption-key identity; plaintext и key material в нём нет.
type DraftEncryptedDescriptor struct {
	Namespace, Name, DataKey, UID, ResourceVersion, CiphertextSHA256 string
	EncryptionKey                                                    DraftEncryptionKey
}

// DraftWork carries claim-fenced intent, scope/content binding, destinations, deadlines and optional
// prior effects from a protected owner RPC. Merely constructing this mutable metadata does not issue
// a grant or prove the claim is current; the receiving service must validate the full snapshot.
// DraftWork переносит claim-fenced intent, scope/content binding, destinations, deadlines и прежние
// эффекты из защищённого owner RPC. Создание этих изменяемых metadata не выдаёт grant и не доказывает
// актуальность claim; принимающий сервис обязан проверить полный snapshot.
type DraftWork struct {
	OperationRef, ClaimantID                                 string
	Kind                                                     DraftOperation
	ClaimGeneration                                          int64
	Draft                                                    SecretDraft
	Binding                                                  SecretDraftBinding
	StagedNamespace, StagedName, StagedKey, RuntimeNamespace string
	TargetRevision                                           int64
	LeaseDeadline, ExpiresAt                                 time.Time
	Encrypted                                                *DraftEncryptedDescriptor
	RecoveryEncrypted                                        *DraftEncryptedDescriptor
	RecoveryMaterialization                                  *DraftMaterialization
}

// DraftMaterialization pins runtime object identity, revision and content digest without its bytes;
// resolving or deleting the object still requires exact owner-authorized effect checks.
// DraftMaterialization закрепляет runtime object identity, revision и content digest без его байтов;
// чтение или удаление требует точных owner-authorized effect checks.
type DraftMaterialization struct {
	Namespace, Name, DataKey, UID, ResourceVersion, ContentSHA256 string
	Revision                                                      int64
}

// PublishedSecret is the owner-confirmed public result metadata, deliberately excluding plaintext
// and the display hint of the immediate API. The struct itself does not authenticate that confirmation.
// PublishedSecret — публичные metadata подтверждённого владельцем результата, намеренно без plaintext
// и display hint immediate API. Сама структура не подтверждает подлинность этого ответа.
type PublishedSecret struct {
	ScopeKind                                             runtimesecret.ScopeKind
	OrganizationRef                                       string
	Ref, ProjectRef, Name, Description, ValueType, Status string
	Version, Revision                                     int64
	CreatedAt, UpdatedAt                                  time.Time
}

// DraftResult returns draft metadata plus optional published-secret metadata from the domain operation;
// it contains no value and does not imply that response casting or external delivery succeeded.
// DraftResult возвращает draft metadata и необязательные published-secret metadata доменной операции;
// значения в нём нет, успех response casting или внешней доставки из него не следует.
type DraftResult struct {
	Draft  SecretDraft
	Secret *PublishedSecret
}

// DraftRecoveryAction labels the owner's decision for one observed effect, not a caller-issued permission;
// unknown string values must still be rejected by the recovery boundary.
// DraftRecoveryAction обозначает owner decision для наблюдаемого эффекта, не выданное caller разрешение;
// неизвестные строковые значения по-прежнему отвергаются recovery boundary.
type DraftRecoveryAction string

const (
	// DraftRecoveryKeep directs reconciliation to retain the exact verified effect selected by the owner.
	// DraftRecoveryKeep предписывает reconciliation сохранить точный проверенный эффект, выбранный owner.
	DraftRecoveryKeep DraftRecoveryAction = "KEEP"
	// DraftRecoveryDelete directs exact-effect cleanup, never arbitrary namespace or resource deletion.
	// DraftRecoveryDelete предписывает точную очистку эффекта, не произвольное удаление namespace/resource.
	DraftRecoveryDelete DraftRecoveryAction = "DELETE"
)

// DraftRecoveryDecision keeps encrypted staging and runtime materialization decisions separate;
// each action is interpreted only against the corresponding verified effect identity.
// DraftRecoveryDecision разделяет решения об encrypted staging и runtime materialization;
// каждое действие применяется лишь к соответствующей проверенной effect identity.
type DraftRecoveryDecision struct {
	EncryptedAction, MaterializationAction DraftRecoveryAction
}
