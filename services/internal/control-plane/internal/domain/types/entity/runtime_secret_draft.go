package entity

import "time"

// RuntimeSecretDraft presents draft metadata, owner scope, lifecycle state, aggregate version,
// generation, publication pins, and lifetime. It contains no draft value and does not authorize
// publication merely because a revision is recorded.
// RuntimeSecretDraft представляет метаданные черновика, область владельца, состояние, версию агрегата,
// поколение, привязки публикации и срок. Значение черновика не передаётся; наличие записанной ревизии
// само по себе не разрешает публикацию.
type RuntimeSecretDraft struct {
	ScopeKind, OrganizationRef                                      string
	Ref, ProjectRef, SecretRef, Name, Description, ValueType, State string
	Version, Generation, PublishedRevision, SecretVersion           int64
	CreatedAt, UpdatedAt, ExpiresAt                                 time.Time
}

// RuntimeSecretDraftEncryptedDescriptor is the internal readback descriptor of encrypted draft
// storage, including exact Secret object pins, ciphertext digest, and encryption-key identity and
// generation. It contains neither plaintext nor the encryption key and is not a public browser
// projection.
// RuntimeSecretDraftEncryptedDescriptor является внутренним дескриптором обратного чтения
// зашифрованного черновика с точными привязками Secret, дайджестом шифротекста, личностью и поколением
// ключа шифрования. Открытый текст и сам ключ отсутствуют; это не публичная проекция браузера.
type RuntimeSecretDraftEncryptedDescriptor struct {
	Namespace, SecretName, SecretKey, SecretUID, SecretResourceVersion string
	CiphertextSHA256, EncryptionKeyID                                  string
	EncryptionKeyGeneration                                            int64
}

// RuntimeSecretDraftWork binds draft processing or recovery to an operation, claimant, generation,
// exact staged and encrypted descriptors, expected digest, target revision, and deadlines. Optional
// recovery descriptors identify cleanup evidence; they do not permit arbitrary object replacement or
// deletion.
// RuntimeSecretDraftWork связывает обработку или восстановление черновика с операцией, исполнителем,
// поколением, точными промежуточными и зашифрованными дескрипторами, ожидаемым дайджестом, целевой
// ревизией и сроками. Необязательные дескрипторы восстановления определяют свидетельства очистки, а не
// разрешают произвольную замену или удаление объектов.
type RuntimeSecretDraftWork struct {
	RecoveryEncrypted                                                                                                    *RuntimeSecretDraftEncryptedDescriptor
	RecoveryMaterialization                                                                                              *RuntimeSecretMaterialization
	OperationRef, Kind, ExpectedContentSHA256, Namespace, StagedNamespace, StagedSecretName, StagedSecretKey, ClaimantID string
	Draft                                                                                                                RuntimeSecretDraft
	TargetRevision, ClaimGeneration                                                                                      int64
	Encrypted                                                                                                            *RuntimeSecretDraftEncryptedDescriptor
	LeaseDeadline, ExpiresAt                                                                                             time.Time
}

// RuntimeSecretDraftOperationReceipt returns the owner's operation grant, identifiers, state,
// deadline, draft, and optional terminal secret metadata. OperationGrant is an opaque capability and
// must not be treated as safe display text or logged with the public receipt fields.
// RuntimeSecretDraftOperationReceipt возвращает разрешение операции владельца, идентификаторы,
// состояние, срок, черновик и необязательные итоговые метаданные секрета. OperationGrant является
// непрозрачной возможностью; его нельзя считать безопасным текстом для показа или журналировать вместе
// с публичными полями квитанции.
type RuntimeSecretDraftOperationReceipt struct {
	OperationGrant, OperationRef, State, FailureCode string
	ExpiresAt                                        time.Time
	Draft                                            RuntimeSecretDraft
	TerminalSecret                                   *RuntimeSecret
}

// RuntimeSecretDraftResult reports the owner-accepted draft outcome with optional published secret and
// encrypted-storage and materialization actions. Completed describes completion of the requested
// operation or cleanup; it is not synonymous with successful publication in every lifecycle branch.
// RuntimeSecretDraftResult сообщает принятый владельцем исход черновика с необязательным
// опубликованным секретом и действиями зашифрованного хранения и материализации. Completed описывает
// завершение запрошенной операции или очистки, а не успешную публикацию в каждой ветви жизненного
// цикла.
type RuntimeSecretDraftResult struct {
	Draft                                         RuntimeSecretDraft
	Secret                                        *RuntimeSecret
	State, EncryptedAction, MaterializationAction string
	Completed                                     bool
}
