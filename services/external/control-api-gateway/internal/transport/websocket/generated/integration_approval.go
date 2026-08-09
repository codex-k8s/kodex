
package generated

type IntegrationApproval struct {
  ApprovalRef string
  InvocationRef string
  Version int
  Status *IntegrationApprovalStatus
  RequestHash string
  RedactedPreview *IntegrationApprovalRedactedPreview
  ExpiresAt string
  DecidedAt string
  ReasonCode string
}