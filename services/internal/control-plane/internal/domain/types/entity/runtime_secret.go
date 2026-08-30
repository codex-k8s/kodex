package entity

import "time"

type RuntimeSecretDisplayHint struct {
	Prefix string
	Suffix string
}

type RuntimeSecret struct {
	Ref, ProjectRef, Name, Description, ValueType, State, Namespace string
	Version, CurrentRevision                                        int64
	DisplayHint                                                     *RuntimeSecretDisplayHint
	CurrentRevisionDescriptor                                       *RuntimeSecretRevisionDescriptor
	CreatedAt, UpdatedAt                                            time.Time
	NextActions                                                     []string
}

type RuntimeSecretOperation struct {
	Ref, Kind, ProjectRef, SecretRef, Name, Description, ValueType string
	Namespace, SecretKey, ExpectedContentSHA256                    string
	TargetRevision, ClaimGeneration                                int64
	VersionedSecretNames                                           []string
	RevisionDescriptors                                            []RuntimeSecretRevisionDescriptor
	ExpiresAt, LeaseDeadline                                       time.Time
}

// RuntimeSecretRecoveryWork содержит только координаты, необходимые
// secret-broker для восстановления истёкшей fenced операции. Значение секрета,
// actor и idempotency metadata остаются внутри control-plane.
type RuntimeSecretRecoveryWork struct {
	OperationRef, Kind, ClaimantID, Namespace, SecretRef, SecretKey, ExpectedContentSHA256 string
	ClaimGeneration, TargetRevision                                                        int64
	LeaseDeadline                                                                          time.Time
}

type RuntimeSecretRevisionDescriptor struct {
	Revision                                                                          int64
	Namespace, SecretName, SecretKey, SecretUID, SecretResourceVersion, ContentSHA256 string
}

type RuntimeSecretMaterialization struct {
	Namespace, SecretName, SecretKey, SecretUID, SecretResourceVersion, ContentSHA256 string
	DisplayHint                                                                       *RuntimeSecretDisplayHint
}
