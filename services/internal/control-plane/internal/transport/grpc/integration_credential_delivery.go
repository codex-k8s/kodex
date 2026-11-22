package grpc

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// castCredentialDelivery maps only closed owner metadata. Unknown/mixed representations remain
// unspecified instead of being interpreted as a token or as a credential-free Connection.
// castCredentialDelivery преобразует только closed owner metadata. Unknown/mixed representations
// остаются unspecified вместо интерпретации как token либо credential-free Connection.
func castCredentialDelivery(delivery integrationpackage.CredentialDelivery) (cp.IntegrationCredentialDeliveryKind, cp.IntegrationBrokerIdentityPurpose) {
	kind := cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_UNSPECIFIED
	purpose := cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_UNSPECIFIED
	switch delivery.Kind {
	case integrationpackage.CredentialDeliveryNone:
		if delivery.BrokerPurpose == "" {
			kind = cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_NONE
		}
	case integrationpackage.CredentialDeliveryToken:
		if delivery.BrokerPurpose == "" {
			kind = cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_TOKEN
		}
	case integrationpackage.CredentialDeliveryBrokerIdentity:
		switch delivery.BrokerPurpose {
		case "KUBERNETES_TARGET":
			purpose = cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_KUBERNETES_TARGET
		case "SSH_TARGET":
			purpose = cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_SSH_TARGET
		}
		if purpose != cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_UNSPECIFIED {
			kind = cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_BROKER_IDENTITY
		}
	}
	return kind, purpose
}
