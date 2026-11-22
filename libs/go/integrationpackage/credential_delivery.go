package integrationpackage

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"go.yaml.in/yaml/v3"
)

// errCredentialMetadata hides malformed representation details instead of reflecting source input.
// errCredentialMetadata скрывает детали malformed representation без отражения source input.
var errCredentialMetadata = errors.New("integration credential metadata is invalid")

// credentialWire avoids recursive custom decoding while retaining field-presence information.
// credentialWire предотвращает recursive custom decoding, сохраняя field-presence information.
type credentialWire Credential

// UnmarshalJSON preserves the difference between an omitted field and an explicitly supplied
// empty/null branch. Duplicate, case-aliased, unknown and trailing fields are rejected before use.
// UnmarshalJSON сохраняет различие omitted field и явно переданной empty/null branch.
// Duplicate, case-aliased, unknown и trailing fields отвергаются до использования.
func (credential *Credential) UnmarshalJSON(raw []byte) error {
	if credential == nil || len(raw) == 0 || len(raw) > maxBytes {
		return errCredentialMetadata
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if uniqueJSONValue(decoder, 0, false) != nil {
		return errCredentialMetadata
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errCredentialMetadata
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return errCredentialMetadata
	}
	for key, rawValue := range fields {
		if key != "kind" && key != "secretKey" && key != "brokerPurpose" {
			return errCredentialMetadata
		}
		var text string
		if bytes.Equal(bytes.TrimSpace(rawValue), []byte("null")) || json.Unmarshal(rawValue, &text) != nil || len(text) > 120 {
			return errCredentialMetadata
		}
	}
	var decoded credentialWire
	if json.Unmarshal(raw, &decoded) != nil {
		return errCredentialMetadata
	}
	_, hasSecretKey := fields["secretKey"]
	_, hasBrokerPurpose := fields["brokerPurpose"]
	if !validCredentialWirePresence(decoded.Kind, hasSecretKey, hasBrokerPurpose) {
		return errCredentialMetadata
	}
	*credential = Credential(decoded)
	return nil
}

// UnmarshalYAML applies the same exact field names and presence semantics as JSON, including
// scalar string types. It does not bypass the outer package's alias/depth/document guards.
// UnmarshalYAML применяет те же exact field names и presence semantics, что JSON, включая
// scalar string types. Внешние package alias/depth/document guards не обходятся.
func (credential *Credential) UnmarshalYAML(node *yaml.Node) error {
	if credential == nil || node == nil || node.Kind != yaml.MappingNode || len(node.Content)%2 != 0 {
		return errCredentialMetadata
	}
	fields := make(map[string]bool, 3)
	for index := 0; index < len(node.Content); index += 2 {
		key, value := node.Content[index], node.Content[index+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || fields[key.Value] ||
			(key.Value != "kind" && key.Value != "secretKey" && key.Value != "brokerPurpose") ||
			value.Kind != yaml.ScalarNode || value.Tag != "!!str" || len(value.Value) > 120 {
			return errCredentialMetadata
		}
		fields[key.Value] = true
	}
	var decoded credentialWire
	if node.Decode(&decoded) != nil {
		return errCredentialMetadata
	}
	if !validCredentialWirePresence(decoded.Kind, fields["secretKey"], fields["brokerPurpose"]) {
		return errCredentialMetadata
	}
	*credential = Credential(decoded)
	return nil
}

// validCredentialWirePresence enforces exactly one branch before canonicalization can omit empties.
// It does not add parser bookkeeping to semantic metadata or change equality of parsed packages.
// validCredentialWirePresence требует ровно одну branch до удаления empties canonicalization.
// Parser bookkeeping не попадает в semantic metadata и не меняет equality parsed packages.
func validCredentialWirePresence(kind string, secretKey, brokerPurpose bool) bool {
	switch kind {
	case "TOKEN", "PASSWORD":
		return secretKey && !brokerPurpose
	case string(CredentialDeliveryBrokerIdentity):
		return brokerPurpose && !secretKey
	default:
		return false
	}
}

// CredentialDeliveryKind distinguishes owner-managed metadata from generic token/password
// descriptors. It describes representation only and does not grant execution or account authority.
// CredentialDeliveryKind отличает owner-managed metadata от generic token/password descriptors.
// Описывает только representation, не выдавая execution либо account authority.
type CredentialDeliveryKind string

const (
	// CredentialDeliveryNone means the existing route owns credentials outside Connection selection.
	// CredentialDeliveryNone означает владение credentials вне Connection selection у прежнего route.
	CredentialDeliveryNone CredentialDeliveryKind = "NONE"
	// CredentialDeliveryToken retains the generic descriptor path for TOKEN and PASSWORD payloads.
	// CredentialDeliveryToken сохраняет generic descriptor path для TOKEN и PASSWORD payloads.
	CredentialDeliveryToken CredentialDeliveryKind = "TOKEN"
	// CredentialDeliveryBrokerIdentity selects only the purpose-specific private broker resolver.
	// CredentialDeliveryBrokerIdentity выбирает только purpose-specific private broker resolver.
	CredentialDeliveryBrokerIdentity CredentialDeliveryKind = "BROKER_IDENTITY"
)

// CredentialDelivery is a safe projection of validated package metadata, not a runtime locator.
// BrokerPurpose remains empty for token/password and credential-free Connection routes.
// CredentialDelivery — safe projection проверенных package metadata, не runtime locator.
// BrokerPurpose остаётся пустым у token/password и credential-free Connection routes.
type CredentialDelivery struct {
	Kind          CredentialDeliveryKind
	BrokerPurpose string
}

// ConnectionCredentialDelivery derives the representation exclusively from a validated pinned
// definition. Callers still resolve current owner bindings and registry readiness separately.
// ConnectionCredentialDelivery выводит representation исключительно из validated pinned definition.
// Caller отдельно разрешает current owner bindings и registry readiness.
func (definition Package) ConnectionCredentialDelivery() (CredentialDelivery, bool) {
	if !validConnectionCredential(definition.Spec.Adapter, definition.Spec.Credential) {
		return CredentialDelivery{}, false
	}
	if !definition.RequiresConnectionCredential() {
		return CredentialDelivery{Kind: CredentialDeliveryNone}, true
	}
	if definition.Spec.Credential.Kind == string(CredentialDeliveryBrokerIdentity) {
		return CredentialDelivery{Kind: CredentialDeliveryBrokerIdentity, BrokerPurpose: definition.Spec.Credential.BrokerPurpose}, true
	}
	return CredentialDelivery{Kind: CredentialDeliveryToken}, true
}

// validConnectionCredential rejects unknown or mixed representations before package acceptance.
// Broker purposes are closed adapter bindings, not arbitrary Secret keys or caller-selected URLs;
// adapter registration and an executable consumer remain mandatory independent checks.
// validConnectionCredential отклоняет unknown/mixed representations до принятия package.
// Broker purposes — closed adapter bindings, не произвольные Secret keys либо caller URLs;
// adapter registration и executable consumer проверяются независимо и остаются обязательными.
func validConnectionCredential(adapter string, credential *Credential) bool {
	if adapter == "KUBERNETES" || adapter == "SSH_TARGET" {
		purpose := "KUBERNETES_TARGET"
		if adapter == "SSH_TARGET" {
			purpose = "SSH_TARGET"
		}
		return credential != nil && credential.Kind == string(CredentialDeliveryBrokerIdentity) &&
			credential.SecretKey == "" && credential.BrokerPurpose == purpose
	}
	if credential == nil {
		return true
	}
	switch credential.Kind {
	case "TOKEN", "PASSWORD":
		return validKey(credential.SecretKey) && credential.BrokerPurpose == ""
	case string(CredentialDeliveryBrokerIdentity):
		return false
	default:
		return false
	}
}
