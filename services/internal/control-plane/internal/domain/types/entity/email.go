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

type EmailExecutionBinding struct {
	InvocationRef, ConnectionTestRef string
	LeaseRef, Fence                  string
	Generation                       int64
	ExpiresAt                        time.Time
}

type EmailAuthorizationScope struct {
	MailboxRef, Sender              string
	Operations, Folders, Recipients []string
}

type EmailAuthorization struct {
	Allowed, GateApproved                                                   bool
	ActorRef, AgentRef, OrganizationRef, ProjectRef, ConnectionRef          string
	MailboxRef, GrantRef, Operation, SemanticInputDigest, EffectKey, Policy string
	ConfigurationRevision, CredentialGeneration                             int64
	UserScope, ConnectionScope, ResourceScope                               EmailAuthorizationScope
	AgentScope                                                              *EmailAuthorizationScope
	ExpiresAt                                                               time.Time
	Binding                                                                 EmailExecutionBinding
}
