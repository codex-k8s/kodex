package entity

import "time"

type RuntimeSecretDraft struct {
	Ref, ProjectRef, SecretRef, Name, Description, ValueType, State string
	Version, Generation, PublishedRevision                          int64
	CreatedAt, UpdatedAt, ExpiresAt                                 time.Time
}

// RuntimeSecretDraftEncryptedDescriptor содержит только внутренний readback.
type RuntimeSecretDraftEncryptedDescriptor struct {
	Namespace, SecretName, SecretKey, SecretUID, SecretResourceVersion string
	CiphertextSHA256, EncryptionKeyID                                  string
	EncryptionKeyGeneration                                            int64
}

type RuntimeSecretDraftWork struct {
	RecoveryEncrypted                                                                                                    *RuntimeSecretDraftEncryptedDescriptor
	RecoveryMaterialization                                                                                              *RuntimeSecretMaterialization
	OperationRef, Kind, ExpectedContentSHA256, Namespace, StagedNamespace, StagedSecretName, StagedSecretKey, ClaimantID string
	Draft                                                                                                                RuntimeSecretDraft
	TargetRevision, ClaimGeneration                                                                                      int64
	Encrypted                                                                                                            *RuntimeSecretDraftEncryptedDescriptor
	LeaseDeadline, ExpiresAt                                                                                             time.Time
}

type RuntimeSecretDraftOperationReceipt struct {
	OperationGrant, OperationRef, State, FailureCode string
	ExpiresAt                                        time.Time
	Draft                                            RuntimeSecretDraft
	TerminalSecret                                   *RuntimeSecret
}

type RuntimeSecretDraftResult struct {
	Draft                                         RuntimeSecretDraft
	Secret                                        *RuntimeSecret
	State, EncryptedAction, MaterializationAction string
	Completed                                     bool
}
