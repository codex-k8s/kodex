package entity

import "time"

type EmailEffectReceipt struct {
	Ref, InvocationRef, ExternalReceiptRef, ExternalReceiptDigest string
	SemanticInputDigest, EffectKey, Outcome, MailboxRef           string
	ConnectionRef, ProjectRef                                     string
	Version, ConfigurationRevision                                int64
	CreatedAt, UpdatedAt                                          time.Time
}

type EmailReconciliationDecision struct {
	Ref, ReceiptRef, ReceiptDigest, InvocationRef string
	Outcome, GrantRef, ActorRef                   string
	Version, ReceiptVersion                       int64
	CreatedAt, ExpiresAt                          time.Time
}

type EmailEffectReceiptView struct {
	Receipt  EmailEffectReceipt
	Decision *EmailReconciliationDecision
}
