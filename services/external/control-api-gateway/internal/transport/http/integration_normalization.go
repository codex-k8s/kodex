package httptransport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

var errPublicIntegrationShape = errors.New("public integration response is invalid")

func normalizeIntegrationEnum(value any, descriptor protoreflect.EnumDescriptor) (any, error) {
	prefix := ""
	switch descriptor.FullName() {
	case "controlplane.v1.IntegrationRisk":
		prefix = "INTEGRATION_RISK_"
	case "controlplane.v1.IntegrationApprovalPolicy":
		prefix = "INTEGRATION_APPROVAL_POLICY_"
	case "controlplane.v1.IntegrationResourceKind":
		prefix = "INTEGRATION_RESOURCE_KIND_"
	case "controlplane.v1.IntegrationDefinitionOrigin":
		prefix = "INTEGRATION_DEFINITION_ORIGIN_"
	default:
		return value, nil
	}
	name, ok := value.(string)
	if !ok {
		return nil, errPublicIntegrationShape
	}
	item := descriptor.Values().ByName(protoreflect.Name(name))
	if item == nil || item.Number() == 0 || !strings.HasPrefix(name, prefix) {
		return nil, errPublicIntegrationShape
	}
	return strings.TrimPrefix(name, prefix), nil
}

func normalizeIntegrationShape(value map[string]any, descriptor protoreflect.MessageDescriptor) error {
	switch descriptor.FullName() {
	case "controlplane.v1.IntegrationCapability", "controlplane.v1.IntegrationGrant":
		schema, hasSchema := value["inputSchema"]
		digest, hasDigest := value["inputSchemaSha256"]
		if hasSchema || hasDigest {
			text, valid := schema.(string)
			pin, digestValid := digest.(string)
			if !hasSchema || !hasDigest || !valid || !digestValid || len(text) > 256<<10 || !json.Valid([]byte(text)) || !validManagedDigest(pin) {
				return errPublicIntegrationShape
			}
			sum := sha256.Sum256([]byte(text))
			if hex.EncodeToString(sum[:]) != pin {
				return errPublicIntegrationShape
			}
		}
		if typed, exists := value["typedRisk"]; exists {
			if risk, present := value["risk"]; present && risk != typed {
				return errPublicIntegrationShape
			}
			value["risk"] = typed
			delete(value, "typedRisk")
		}
		if risk, exists := value["risk"]; exists {
			switch risk {
			case "READ", "WRITE", "SENSITIVE", "DESTRUCTIVE":
			default:
				return errPublicIntegrationShape
			}
		}
	case "controlplane.v1.IntegrationConfigurationField":
		for _, pair := range [][2]string{{"minimum", "hasMinimum"}, {"maximum", "hasMaximum"}} {
			present, _ := value[pair[1]].(bool)
			if !present {
				delete(value, pair[0])
			} else if _, exists := value[pair[0]]; !exists {
				value[pair[0]] = float64(0)
			}
			delete(value, pair[1])
		}
	}
	return nil
}
