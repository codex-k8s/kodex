package value

import "time"

type DraftOperation string

const (
	DraftSave     DraftOperation = "SAVE"
	DraftValidate DraftOperation = "VALIDATE"
	DraftPublish  DraftOperation = "PUBLISH"
	DraftDiscard  DraftOperation = "DISCARD"
)

// SecretDraft содержит только публичную metadata владельца.
type SecretDraft struct {
	Ref, ProjectRef, SecretRef, Name, Description, ValueType, State string
	Version, Generation, PublishedRevision, SecretVersion           int64
	CreatedAt, UpdatedAt, ExpiresAt                                 time.Time
}

type DraftEncryptedDescriptor struct {
	Namespace, Name, DataKey, UID, ResourceVersion, CiphertextSHA256 string
	EncryptionKey                                                    DraftEncryptionKey
}

// DraftWork принимается исключительно от защищённого owner RPC.
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

type DraftMaterialization struct {
	Namespace, Name, DataKey, UID, ResourceVersion, ContentSHA256 string
	Revision                                                      int64
}

// PublishedSecret намеренно не переносит display hint из старого immediate API.
type PublishedSecret struct {
	Ref, ProjectRef, Name, Description, ValueType, Status string
	Version, Revision                                     int64
	CreatedAt, UpdatedAt                                  time.Time
}

type DraftResult struct {
	Draft  SecretDraft
	Secret *PublishedSecret
}

type DraftRecoveryAction string

const (
	DraftRecoveryKeep   DraftRecoveryAction = "KEEP"
	DraftRecoveryDelete DraftRecoveryAction = "DELETE"
)

type DraftRecoveryDecision struct {
	EncryptedAction, MaterializationAction DraftRecoveryAction
}
