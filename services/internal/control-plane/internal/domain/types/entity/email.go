package entity

import "time"

// EmailEffectReceipt records a scoped email invocation's exact semantic input, effect key, external
// receipt and digest, configuration revision, and reported outcome. Unknown outcomes remain evidence
// to reconcile rather than permission for a blind resend.
// EmailEffectReceipt фиксирует точные семантический вход, ключ эффекта, внешнюю квитанцию и дайджест,
// ревизию конфигурации и сообщённый исход почтового вызова в области владельца. Неизвестный исход
// остаётся предметом сверки, а не разрешением слепой повторной отправки.
type EmailEffectReceipt struct {
	Ref, InvocationRef, ExternalReceiptRef, ExternalReceiptDigest string
	SemanticInputDigest, EffectKey, Outcome, MailboxRef           string
	ConnectionRef, ProjectRef                                     string
	Version, ConfigurationRevision                                int64
	CreatedAt, UpdatedAt                                          time.Time
}

// EmailReconciliationDecision pins an actor's reconciliation outcome and bounded grant to an exact
// receipt digest and version. It is distinct from approval of an outgoing message and cannot be reused
// for changed semantic input.
// EmailReconciliationDecision закрепляет исход сверки субъекта и ограниченное разрешение за точными
// дайджестом и версией квитанции. Это отличается от подтверждения исходящего сообщения и не
// переиспользуется при изменённом семантическом входе.
type EmailReconciliationDecision struct {
	Ref, ReceiptRef, ReceiptDigest, InvocationRef string
	Outcome, GrantRef, ActorRef                   string
	Version, ReceiptVersion                       int64
	CreatedAt, ExpiresAt                          time.Time
}

// EmailEffectReceiptView presents an email effect receipt with an optional reconciliation decision.
// Absence of a decision is not confirmation of delivery, failure, or permission to repeat the effect.
// EmailEffectReceiptView представляет квитанцию почтового эффекта с необязательным решением сверки.
// Отсутствие решения не подтверждает доставку, отказ или разрешение повторить эффект.
type EmailEffectReceiptView struct {
	Receipt  EmailEffectReceipt
	Decision *EmailReconciliationDecision
}

// EmailExecutionBinding ties a mail operation to an invocation or connection test, exact lease, fence,
// generation, and expiry. The owner validates the current execution binding; copied identifiers are
// not sufficient authorization.
// EmailExecutionBinding связывает почтовую операцию с вызовом или проверкой подключения, точными
// арендой, барьером, поколением и сроком. Владелец проверяет текущую привязку исполнения;
// скопированные идентификаторы не являются достаточными полномочиями.
type EmailExecutionBinding struct {
	InvocationRef, ConnectionTestRef string
	LeaseRef, Fence                  string
	Generation                       int64
	ExpiresAt                        time.Time
}

// EmailAuthorizationScope describes mailbox, sender, operation, folder, and recipient restrictions for
// one layer of mail authorization. Multiple scopes are checked together; this structure does not
// combine them or validate an address by itself.
// EmailAuthorizationScope описывает ограничения ящика, отправителя, операций, папок и получателей
// одного слоя почтовой авторизации. Несколько областей проверяются вместе; сама структура не
// объединяет их и не проверяет адрес.
type EmailAuthorizationScope struct {
	MailboxRef, Sender              string
	Operations, Folders, Recipients []string
}

// EmailAuthorization carries the owner's scoped email decision with exact actor, connection, grant,
// operation, semantic input, effect, configuration, credentials generation, execution binding, and
// expiry. Allowed and GateApproved are decision results, not caller-controlled substitutes for a
// server-owned human decision.
// EmailAuthorization передаёт решение владельца о почте с точными субъектом, подключением,
// разрешением, операцией, семантическим входом, эффектом, конфигурацией, поколением реквизитов,
// исполнением и сроком. Allowed и GateApproved являются результатами решения, а не заменой серверному
// решению человека от вызывающего кода.
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
