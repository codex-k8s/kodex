package platform

import (
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// credentialDeliveryForExactPackageSnapshot derives representation from an exact explicit package
// kind, never from a missing receipt field or generic Secret descriptor. Published content is
// supplied by the same owner-qualified catalog snapshot; no per-row lookup or external I/O occurs.
// credentialDeliveryForExactPackageSnapshot выводит representation из exact explicit package kind,
// не из отсутствующего receipt field либо generic Secret descriptor. Published content приходит
// из того же owner-qualified catalog snapshot без per-row lookup либо external I/O.
func (repository *Repository) credentialDeliveryForExactPackageSnapshot(key, version, digest, format, content string) (integrationpackage.CredentialDelivery, error) {
	current, registered := repository.integrationDefinitions[key]
	if format == "" && content == "" {
		if registered {
			if exact, ok := integrationpackage.ResolveShippedRevision(current, version, digest); ok {
				delivery, valid := exact.ConnectionCredentialDelivery()
				if !valid {
					return integrationpackage.CredentialDelivery{}, errs.ErrUnavailable
				}
				return delivery, nil
			}
		}
		return integrationpackage.CredentialDelivery{}, nil
	}
	if format != "JSON" && format != "YAML" || content == "" {
		return integrationpackage.CredentialDelivery{}, errs.ErrUnavailable
	}
	definition, err := integrationpackage.Parse([]byte(content))
	if err != nil || definition.Metadata.Key != key || definition.Metadata.Version != version || definition.Digest != digest ||
		registered && integrationpackage.ValidateExecutableRevision(definition, current) != nil {
		return integrationpackage.CredentialDelivery{}, errs.ErrUnavailable
	}
	delivery, valid := definition.ConnectionCredentialDelivery()
	if !valid {
		return integrationpackage.CredentialDelivery{}, errs.ErrUnavailable
	}
	delivery.BrokerPurpose = strings.Clone(delivery.BrokerPurpose)
	return delivery, nil
}
