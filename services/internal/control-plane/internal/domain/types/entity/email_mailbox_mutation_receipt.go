package entity

// EmailMailboxMutationReceipt contains the authenticated actor's historical owner mutation result, not current eligibility or permission to repeat an external email effect.
// EmailMailboxMutationReceipt содержит исторический результат owner mutation аутентифицированного actor, а не текущую eligibility либо право повторить внешний почтовый эффект.
type EmailMailboxMutationReceipt struct {
	Operation string
	Mailbox   *MyEmailMailbox
	Ownership *EmailMailboxOwnershipView
}
