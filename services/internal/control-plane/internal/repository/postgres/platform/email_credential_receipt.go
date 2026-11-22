package platform

import _ "embed"

// queryEmailCredentialReceiptCurrent prevents a historical setup receipt from confirming a
// descriptor belonging to a different mailbox owner epoch; it never reads a credential value.
// queryEmailCredentialReceiptCurrent не позволяет исторической setup-квитанции подтвердить
// descriptor другого поколения владельца mailbox; значение credentials не читается.
//
//go:embed sql/email_credential__receipt_current.sql
var queryEmailCredentialReceiptCurrent string
