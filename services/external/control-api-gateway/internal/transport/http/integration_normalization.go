package httptransport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// errPublicIntegrationShape is the fixed error for invalid integration response shapes, keeping
// arbitrary provider and credential data out of the error text.
// errPublicIntegrationShape задаёт фиксированную ошибку неверной формы ответа интеграции без
// произвольных данных провайдера и credentials в тексте ошибки.
var errPublicIntegrationShape = errors.New("public integration response is invalid")

// normalizeIntegrationEnum maps only owned integration/gate enums to public suffixes after
// descriptor lookup rejects zero/unknown values. Other enum descriptors pass through unchanged for
// their own normalizer.
// normalizeIntegrationEnum преобразует только принадлежащие этому пути integration/gate enum в
// публичные суффиксы после отклонения нулевых и неизвестных значений по descriptor. Прочие enum
// передаются без изменений собственному normalizer.
func normalizeIntegrationEnum(value any, descriptor protoreflect.EnumDescriptor) (any, error) {
	prefix := ""
	switch descriptor.FullName() {
	case "controlplane.v1.IntegrationApprovalConsentState":
		prefix = "INTEGRATION_APPROVAL_CONSENT_STATE_"
	case "controlplane.v1.IntegrationCredentialPolicy":
		prefix = "INTEGRATION_CREDENTIAL_POLICY_"
	case "controlplane.v1.PersonalIntegrationCredentialState":
		prefix = "PERSONAL_INTEGRATION_CREDENTIAL_STATE_"
	case "controlplane.v1.RepositoryBundlePublicationState":
		prefix = "REPOSITORY_BUNDLE_PUBLICATION_STATE_"
	case "controlplane.v1.IntegrationRisk":
		prefix = "INTEGRATION_RISK_"
	case "controlplane.v1.IntegrationApprovalPolicy":
		prefix = "INTEGRATION_APPROVAL_POLICY_"
	case "controlplane.v1.IntegrationApprovalScopeLifetime":
		prefix = "INTEGRATION_APPROVAL_SCOPE_LIFETIME_"
	case "controlplane.v1.IntegrationResourceKind":
		prefix = "INTEGRATION_RESOURCE_KIND_"
	case "controlplane.v1.IntegrationDefinitionOrigin":
		prefix = "INTEGRATION_DEFINITION_ORIGIN_"
	case "controlplane.v1.IntegrationDefinitionConfigurationBinding.State":
		prefix = "STATE_"
	case "controlplane.v1.OwnerGateDecision":
		prefix = "OWNER_GATE_DECISION_"
	case "controlplane.v1.OwnerGateState":
		prefix = "OWNER_GATE_STATE_"
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

// normalizeIntegrationShape normalizes selected integration projections in place: removes private
// revision pins, validates configuration binding shape and schema digest, reconciles typed risk
// and preserves optional numeric-bound presence. OwnerGate validation is delegated; this
// projection path never grants capabilities.
// normalizeIntegrationShape нормализует выбранные проекции интеграций на месте: убирает приватные
// pins ревизий, проверяет форму configuration binding и дайджест схемы, согласует typed risk и
// сохраняет наличие числовых границ. Проверка OwnerGate делегируется; этот путь проекции не выдаёт
// capabilities.
func normalizeIntegrationShape(value map[string]any, descriptor protoreflect.MessageDescriptor) error {
	switch descriptor.FullName() {
	case "controlplane.v1.IntegrationApprovalConsent":
		raw, ok := value["scopeValuesJson"].(string)
		if !ok || len(raw) > 65536 || !json.Valid([]byte(raw)) {
			return errPublicIntegrationShape
		}
		var scopeValues []map[string]any
		if json.Unmarshal([]byte(raw), &scopeValues) != nil || scopeValues == nil || len(scopeValues) > 16 {
			return errPublicIntegrationShape
		}
		value["scopeValuesJSON"] = raw
		delete(value, "scopeValuesJson")
		for _, key := range []string{"version", "maxEffects", "reservedEffects"} {
			number, ok := value[key].(float64)
			if !ok || number < 1 || number > float64(maximumSafeJSONInteger) || number != float64(int64(number)) {
				return errPublicIntegrationShape
			}
		}
		maxEffects := value["maxEffects"].(float64)
		reserved := value["reservedEffects"].(float64)
		if maxEffects > 10000 || reserved > maxEffects {
			return errPublicIntegrationShape
		}
	case "controlplane.v1.ListIntegrationApprovalConsentsResponse":
		items, ok := value["consents"].([]any)
		if !ok || len(items) > 100 {
			return errPublicIntegrationShape
		}
	case "controlplane.v1.IntegrationApprovalScopeContract":
		if !validApprovalScopeContractProjection(value) {
			return errPublicIntegrationShape
		}
	case "controlplane.v1.ListPersonalIntegrationConnectionsResponse":
		items, ok := value["connections"].([]any)
		page, pageOK := value["page"].(map[string]any)
		if !ok || len(items) > 100 || !pageOK {
			return errPublicIntegrationShape
		}
		if token, present := page["nextPageToken"]; present {
			text, valid := token.(string)
			if !valid || len(text) > 512 {
				return errPublicIntegrationShape
			}
		}
		seen := make(map[string]bool, len(items))
		for _, item := range items {
			connection, valid := item.(map[string]any)
			if !valid {
				return errPublicIntegrationShape
			}
			ref, _ := connection["connectionRef"].(string)
			if ref == "" || seen[ref] {
				return errPublicIntegrationShape
			}
			seen[ref] = true
		}
	case "controlplane.v1.PersonalIntegrationConnection":
		ref, refOK := value["connectionRef"].(string)
		version, versionOK := value["connectionVersion"].(float64)
		name, nameOK := value["name"].(string)
		definition, definitionOK := value["definitionKey"].(string)
		_, enabledOK := value["enabled"].(bool)
		_, availableOK := value["available"].(bool)
		if !refOK || !fileTargetRef(ref) || !versionOK || version < 1 || version > float64(maximumSafeJSONInteger) || version != float64(int64(version)) ||
			!nameOK || len(name) == 0 || len([]rune(name)) > 160 || !definitionOK || len(definition) == 0 || len(definition) > 100 || !enabledOK || !availableOK ||
			(value["policy"] != "PROJECT_SHARED" && value["policy"] != "INITIATOR_REQUIRED") {
			return errPublicIntegrationShape
		}
		if credential, present := value["credential"]; present {
			binding, valid := credential.(map[string]any)
			if !valid || binding["connectionRef"] != ref || binding["connectionVersion"] != version || binding["policy"] != value["policy"] {
				return errPublicIntegrationShape
			}
		}
	case "controlplane.v1.SetIntegrationCredentialPolicyResponse":
		if _, ok := value["connection"].(map[string]any); !ok {
			return errPublicIntegrationShape
		}
	case "controlplane.v1.TestPersonalIntegrationCredentialResponse", "controlplane.v1.GetPersonalIntegrationCredentialResponse", "controlplane.v1.CreatePersonalIntegrationCredentialResponse",
		"controlplane.v1.ConfigurePersonalIntegrationCredentialResponse", "controlplane.v1.RevokePersonalIntegrationCredentialResponse":
		if _, ok := value["credential"].(map[string]any); !ok {
			return errPublicIntegrationShape
		}
	case "controlplane.v1.PersonalIntegrationCredentialView":
		for _, key := range []string{"connectionRef", "bindingRef"} {
			ref, ok := value[key].(string)
			if !ok || !fileTargetRef(ref) {
				return errPublicIntegrationShape
			}
		}
		for _, key := range []string{"version", "connectionVersion"} {
			version, ok := value[key].(float64)
			if !ok || version < 1 || version > float64(maximumSafeJSONInteger) || version != float64(int64(version)) {
				return errPublicIntegrationShape
			}
		}
		if value["policy"] != "PROJECT_SHARED" && value["policy"] != "INITIATOR_REQUIRED" {
			return errPublicIntegrationShape
		}
		switch value["state"] {
		case "NOT_CONFIGURED", "CHECK_REQUIRED", "CHECKING", "READY", "INVALID", "REVOKED", "EXPIRED":
		default:
			return errPublicIntegrationShape
		}
		configured, configuredKnown := value["credentialConfigured"].(bool)
		if !configuredKnown || value["state"] == "NOT_CONFIGURED" && configured ||
			value["state"] != "NOT_CONFIGURED" && value["state"] != "REVOKED" && !configured {
			return errPublicIntegrationShape
		}
		for _, key := range []string{"verifiedCapabilityKeys", "deniedCapabilityKeys"} {
			items, ok := value[key].([]any)
			if !ok || len(items) > 256 {
				return errPublicIntegrationShape
			}
			for _, item := range items {
				name, ok := item.(string)
				if !ok || len(name) == 0 || len(name) > 100 || strings.TrimSpace(name) != name {
					return errPublicIntegrationShape
				}
			}
		}
	case "controlplane.v1.OwnerGate":
		return validateOwnerGateProjection(value)
	case "controlplane.v1.IntegrationConnection":
		delete(value, "credentialRevision")
		if value["credentialPolicy"] != "PROJECT_SHARED" && value["credentialPolicy"] != "INITIATOR_REQUIRED" {
			return errPublicIntegrationShape
		}
		if value["credentialPolicy"] == "INITIATOR_REQUIRED" && value["credentialsConfigured"] == true {
			return errPublicIntegrationShape
		}
	case "controlplane.v1.IntegrationDefinitionConfigurationBinding":
		switch value["state"] {
		case "ABSENT":
			if len(value) != 1 {
				return errPublicIntegrationShape
			}
		case "MATCH":
			configurationRef, configurationOK := value["configurationRef"].(string)
			revisionRef, revisionOK := value["revisionRef"].(string)
			version, versionOK := value["bindingVersion"].(float64)
			if len(value) != 4 || !configurationOK || !fileTargetRef(configurationRef) || !revisionOK || !fileTargetRef(revisionRef) ||
				!versionOK || version < 1 || version > float64(maximumSafeJSONInteger) || version != float64(int64(version)) {
				return errPublicIntegrationShape
			}
		default:
			return errPublicIntegrationShape
		}
	case "controlplane.v1.IntegrationCapability", "controlplane.v1.IntegrationGrant":
		if descriptor.FullName() == "controlplane.v1.IntegrationGrant" {
			// Публичный pin принадлежит parent connection; runtime pin остаётся в Proto.
			delete(value, "connectionVersion")
		}
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

// validApprovalScopeContractProjection rejects malformed owner metadata before it can render an incomplete or misleading human-consent selector.
// validApprovalScopeContractProjection отклоняет malformed owner metadata до отображения неполного либо вводящего в заблуждение human-consent selector.
func validApprovalScopeContractProjection(value map[string]any) bool {
	if !gateText(value["name"], 1, 128) {
		return false
	}
	duration, durationOK := value["maxDurationSeconds"].(float64)
	effects, effectsOK := value["maxEffects"].(float64)
	if !durationOK || duration < 1 || duration > 604800 || duration != float64(int64(duration)) || !effectsOK || effects < 1 || effects > 10000 || effects != float64(int64(effects)) {
		return false
	}
	paths, pathsOK := value["requiredInputPaths"].([]any)
	lifetimes, lifetimesOK := value["allowedLifetimes"].([]any)
	if !pathsOK || len(paths) > 16 || !lifetimesOK || len(lifetimes) < 1 || len(lifetimes) > 2 {
		return false
	}
	seen := map[string]bool{}
	for _, raw := range paths {
		path, ok := raw.(string)
		if !ok || len(path) < 2 || len(path) > 256 || path[0] != '/' || seen[path] {
			return false
		}
		seen[path] = true
	}
	seen = map[string]bool{}
	for _, raw := range lifetimes {
		lifetime, ok := raw.(string)
		if !ok || lifetime != "ROOT_RUN" && lifetime != "INITIATOR_RESOURCE" || seen[lifetime] {
			return false
		}
		seen[lifetime] = true
	}
	return true
}
