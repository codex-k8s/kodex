package httptransport

import "google.golang.org/protobuf/reflect/protoreflect"

// normalizeCredentialDeliveryShape packs flat protobuf metadata into the public closed union.
// Absence stays unknown; mixed, partial and foreign-policy broker locators fail before output.
// normalizeCredentialDeliveryShape преобразует flat protobuf metadata в public closed union.
// Отсутствие остаётся unknown; mixed, partial и foreign-policy broker locators отвергаются до output.
func normalizeCredentialDeliveryShape(value map[string]any, descriptor protoreflect.MessageDescriptor) error {
	switch descriptor.FullName() {
	case "controlplane.v1.IntegrationDefinition", "controlplane.v1.IntegrationConnection", "controlplane.v1.PersonalIntegrationCredentialView", "controlplane.v1.PersonalIntegrationConnection":
	default:
		return nil
	}
	kind, hasKind := value["credentialDeliveryKind"]
	purpose, hasPurpose := value["brokerIdentityPurpose"]
	identityRef, hasRef := value["brokerIdentityRef"]
	revision, hasRevision := value["brokerIdentityRevision"]
	_, hasGeneration := value["bindingGeneration"]
	if !hasKind {
		if hasPurpose || hasRef || hasRevision || hasGeneration {
			return errPublicIntegrationShape
		}
		return nil
	}
	delivery := map[string]any{"kind": kind}
	switch kind {
	case "NONE", "TOKEN":
		if hasPurpose || hasRef || hasRevision || descriptor.FullName() == "controlplane.v1.PersonalIntegrationCredentialView" && kind == "NONE" {
			return errPublicIntegrationShape
		}
	case "BROKER_IDENTITY":
		if !hasPurpose || purpose != "KUBERNETES_TARGET" && purpose != "SSH_TARGET" {
			return errPublicIntegrationShape
		}
		delivery["brokerPurpose"] = purpose
	default:
		return errPublicIntegrationShape
	}
	if descriptor.FullName() == "controlplane.v1.PersonalIntegrationCredentialView" {
		if !positiveCredentialMetadataInteger(value["bindingGeneration"]) {
			return errPublicIntegrationShape
		}
	}
	if hasRef != hasRevision {
		return errPublicIntegrationShape
	}
	if hasRef {
		ref, ok := identityRef.(string)
		if kind != "BROKER_IDENTITY" || !ok || !fileTargetRef(ref) || !positiveCredentialMetadataInteger(revision) ||
			descriptor.FullName() == "controlplane.v1.IntegrationDefinition" ||
			descriptor.FullName() == "controlplane.v1.IntegrationConnection" && value["credentialPolicy"] != "PROJECT_SHARED" {
			return errPublicIntegrationShape
		}
		value["brokerIdentity"] = map[string]any{"ref": ref, "revision": revision}
	}
	if kind == "BROKER_IDENTITY" {
		configuredField := "credentialsConfigured"
		if descriptor.FullName() == "controlplane.v1.PersonalIntegrationCredentialView" {
			configuredField = "credentialConfigured"
		}
		if value[configuredField] == true && !hasRef {
			return errPublicIntegrationShape
		}
		if value["credentialRevision"] != nil {
			return errPublicIntegrationShape
		}
	}
	value["credentialDelivery"] = delivery
	for _, field := range []string{"credentialDeliveryKind", "brokerIdentityPurpose", "brokerIdentityRef", "brokerIdentityRevision"} {
		delete(value, field)
	}
	return nil
}

// positiveCredentialMetadataInteger accepts only safe positive JSON counters, without coercion.
// positiveCredentialMetadataInteger принимает только safe positive JSON counters без coercion.
func positiveCredentialMetadataInteger(value any) bool {
	number, ok := value.(float64)
	return ok && number >= 1 && number <= float64(maximumSafeJSONInteger) && number == float64(int64(number))
}
