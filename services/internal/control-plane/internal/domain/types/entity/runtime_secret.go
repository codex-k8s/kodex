package entity

import "time"

// RuntimeSecretDisplayHint contains the explicitly prepared prefix and suffix used to identify a
// secret in authorized views. These partial values are not the full secret, but still require
// appropriate display and logging boundaries.
// RuntimeSecretDisplayHint содержит явно подготовленные префикс и суффикс для распознавания секрета в
// разрешённых представлениях. Частичные значения не являются полным секретом, но всё равно требуют
// надлежащих границ показа и журналирования.
type RuntimeSecretDisplayHint struct {
	Prefix string
	Suffix string
}

// RuntimeSecret presents versioned secret metadata, owner scope, optional display hint, and the
// current revision descriptor without raw secret bytes. NextActions describes the view; executing an
// action requires a fresh owner-side authorization and lifecycle check.
// RuntimeSecret представляет версионированные метаданные секрета, область владельца, необязательную
// подсказку и дескриптор текущей ревизии без исходных байтов секрета. NextActions описывает
// представление; исполнение действия требует новой проверки полномочий и жизненного цикла владельцем.
type RuntimeSecret struct {
	ScopeKind, OrganizationRef                                      string
	Ref, ProjectRef, Name, Description, ValueType, State, Namespace string
	Version, CurrentRevision                                        int64
	DisplayHint                                                     *RuntimeSecretDisplayHint
	CurrentRevisionDescriptor                                       *RuntimeSecretRevisionDescriptor
	CreatedAt, UpdatedAt                                            time.Time
	NextActions                                                     []string
}

// RuntimeSecretOperation describes an owner-created secret operation with exact target revision,
// generation, expected content digest, namespace, and revision descriptors. It is internal
// coordination data, not an arbitrary Secret locator or a substitute for an operation grant.
// RuntimeSecretOperation описывает созданную владельцем операцию секрета с точными целевой ревизией,
// поколением, ожидаемым дайджестом, пространством имён и дескрипторами ревизий. Это внутренние данные
// координации, а не произвольный адрес Secret или замена разрешению операции.
type RuntimeSecretOperation struct {
	ScopeKind, OrganizationRef                                     string
	Ref, Kind, ProjectRef, SecretRef, Name, Description, ValueType string
	Namespace, SecretKey, ExpectedContentSHA256                    string
	TargetRevision, ClaimGeneration                                int64
	VersionedSecretNames                                           []string
	RevisionDescriptors                                            []RuntimeSecretRevisionDescriptor
	ExpiresAt, LeaseDeadline                                       time.Time
}

// RuntimeSecretRecoveryWork carries only the coordinates and claim pins needed by secret-broker to
// recover an expired fenced operation. Secret bytes, actor, and idempotency metadata remain within
// control-plane; the worker must still match the owner's current claim.
// RuntimeSecretRecoveryWork передаёт только координаты и привязки захвата, необходимые secret-broker
// для восстановления истёкшей ограждённой операции. Байты секрета, субъект и метаданные
// идемпотентности остаются в control-plane; исполнитель проверяет совпадение текущего захвата
// владельца.
type RuntimeSecretRecoveryWork struct {
	OperationRef, Kind, ClaimantID, Namespace, SecretRef, SecretKey, ExpectedContentSHA256 string
	ClaimGeneration, TargetRevision                                                        int64
	LeaseDeadline                                                                          time.Time
}

// RuntimeSecretRevisionDescriptor pins one logical secret revision to the exact Kubernetes namespace,
// name, key, UID, resource version, and content digest. It describes an object to verify, not
// authority to read arbitrary Secret bytes.
// RuntimeSecretRevisionDescriptor закрепляет логическую ревизию секрета за точными пространством имён
// Kubernetes, именем, ключом, UID, версией ресурса и дайджестом. Это описание проверяемого объекта, а
// не право читать произвольные байты Secret.
type RuntimeSecretRevisionDescriptor struct {
	Revision                                                                          int64
	Namespace, SecretName, SecretKey, SecretUID, SecretResourceVersion, ContentSHA256 string
}

// RuntimeSecretMaterialization reports the exact created Secret descriptor and optional prepared
// display hint. The owner verifies object and content pins before accepting it; supplying this
// structure does not itself publish a logical secret revision.
// RuntimeSecretMaterialization сообщает точный дескриптор созданного Secret и необязательную
// подготовленную подсказку. Владелец проверяет привязки объекта и содержимого до принятия; передача
// структуры сама по себе не публикует логическую ревизию секрета.
type RuntimeSecretMaterialization struct {
	Namespace, SecretName, SecretKey, SecretUID, SecretResourceVersion, ContentSHA256 string
	DisplayHint                                                                       *RuntimeSecretDisplayHint
}
