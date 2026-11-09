package command

// RevokeIntegrationApprovalConsent is the dedicated self-owned revocation command; project administration does not substitute the original initiator.
// RevokeIntegrationApprovalConsent — отдельная команда отзыва собственного consent; project administration не подменяет original initiator.
const RevokeIntegrationApprovalConsent Kind = "REVOKE_INTEGRATION_APPROVAL_CONSENT"

// IntegrationApprovalConsentInput carries only an opaque resource locator; the owner derives actor and tenant from verified transport.
// IntegrationApprovalConsentInput содержит лишь opaque resource locator; actor и tenant выводятся владельцем из verified transport.
type IntegrationApprovalConsentInput struct{ ConsentRef string }
