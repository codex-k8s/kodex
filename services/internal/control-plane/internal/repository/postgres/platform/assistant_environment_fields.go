package platform

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func assistantEnvironmentSnapshotSpecification(raw any) (entity.RuntimeEnvironmentDraftSpecification, bool) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return entity.RuntimeEnvironmentDraftSpecification{}, false
	}
	var specification entity.RuntimeEnvironmentDraftSpecification
	if json.Unmarshal(encoded, &specification) != nil {
		return entity.RuntimeEnvironmentDraftSpecification{}, false
	}
	return specification, true
}

func assistantEnvironmentValuesForUpdate(snapshot any, input map[string]any) ([]entity.RuntimeEnvironmentValue, bool) {
	specification, valid := assistantEnvironmentSnapshotSpecification(snapshot)
	if !valid {
		return nil, false
	}
	if _, supplied := input["publicValues"]; supplied {
		return assistantEnvironmentPublicValues(input)
	}
	return specification.Values, true
}

func assistantEnvironmentValuesMatch(snapshot any, values []entity.RuntimeEnvironmentValue) bool {
	specification, valid := assistantEnvironmentSnapshotSpecification(snapshot)
	return valid && reflect.DeepEqual(specification.Values, values)
}

func assistantEnvironmentBindingsMatch(snapshot any, bindings []entity.RuntimeSecretBinding) bool {
	specification, valid := assistantEnvironmentSnapshotSpecification(snapshot)
	return valid && reflect.DeepEqual(specification.SecretBindings, bindings)
}

func assistantEnvironmentExistingBindingsCompatible(snapshot any, input map[string]any) bool {
	specification, valid := assistantEnvironmentSnapshotSpecification(snapshot)
	if !valid {
		return false
	}
	values, valid := assistantEnvironmentPublicValues(input)
	return valid && assistantEnvironmentBindingsCompatible(values, specification.SecretBindings)
}

func assistantEnvironmentBindingsCompatible(values []entity.RuntimeEnvironmentValue, bindings []entity.RuntimeSecretBinding) bool {
	if len(bindings) > 128 {
		return false
	}
	seen := make(map[string]struct{}, len(values)+len(bindings))
	for _, value := range values {
		seen[value.Name] = struct{}{}
	}
	for _, binding := range bindings {
		if !runtimecontract.ValidRuntimeEnvironmentName(binding.Name) ||
			!assistantEnvironmentSecretRefValid(binding.SecretRef) || binding.Revision < 0 {
			return false
		}
		if _, duplicate := seen[binding.Name]; duplicate {
			return false
		}
		seen[binding.Name] = struct{}{}
	}
	return true
}

func assistantEnvironmentSecretRefValid(secretRef string) bool {
	if !strings.HasPrefix(secretRef, "sec_") || len(secretRef) < 8 || len(secretRef) > 96 {
		return false
	}
	for _, char := range secretRef {
		if !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') &&
			!(char >= '0' && char <= '9') && char != '_' && char != '-' {
			return false
		}
	}
	return true
}

var assistantSensitiveVariableFragments = []string{
	"SECRET", "PASSWORD", "TOKEN", "CREDENTIAL", "PRIVATE_KEY", "API_KEY",
}

func assistantEnvironmentPublicValues(input map[string]any) ([]entity.RuntimeEnvironmentValue, bool) {
	raw, supplied := input["publicValues"]
	if !supplied {
		return nil, true
	}
	entries, ok := raw.([]any)
	if !ok || len(entries) > 128 {
		return nil, false
	}
	values := make([]entity.RuntimeEnvironmentValue, 0, len(entries))
	contractValues := make([]runtimecontract.RuntimeEnvironmentValue, 0, len(entries))
	for _, entry := range entries {
		item, valid := entry.(map[string]any)
		if !valid || !onlyAssistantFields(item, "name", "value") || !hasAssistantFields(item, "name", "value") {
			return nil, false
		}
		name, nameOK := item["name"].(string)
		value, valueOK := item["value"].(string)
		if !nameOK || !valueOK || name != strings.TrimSpace(name) {
			return nil, false
		}
		for _, sensitive := range assistantSensitiveVariableFragments {
			if strings.Contains(name, sensitive) {
				return nil, false
			}
		}
		values = append(values, entity.RuntimeEnvironmentValue{Name: name, Value: value})
		contractValues = append(contractValues, runtimecontract.RuntimeEnvironmentValue{Name: name, Value: value})
	}
	if runtimecontract.ValidateRuntimeEnvironment(contractValues, nil) != nil {
		return nil, false
	}
	return values, true
}

func assistantEnvironmentSecretBindings(input map[string]any, values []entity.RuntimeEnvironmentValue) ([]entity.RuntimeSecretBinding, bool) {
	raw, supplied := input["secretBindings"]
	if !supplied {
		return nil, true
	}
	entries, ok := raw.([]any)
	if !ok || len(entries) > 128 {
		return nil, false
	}
	seen := make(map[string]struct{}, len(values)+len(entries))
	for _, value := range values {
		seen[value.Name] = struct{}{}
	}
	bindings := make([]entity.RuntimeSecretBinding, 0, len(entries))
	for _, entry := range entries {
		item, valid := entry.(map[string]any)
		if !valid || !onlyAssistantFields(item, "name", "secretRef", "revision") ||
			!hasAssistantFields(item, "name", "secretRef") {
			return nil, false
		}
		name, nameOK := item["name"].(string)
		secretRef, refOK := item["secretRef"].(string)
		if !nameOK || !refOK || !runtimecontract.ValidRuntimeEnvironmentName(name) ||
			!assistantEnvironmentSecretRefValid(secretRef) {
			return nil, false
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, false
		}
		seen[name] = struct{}{}
		var revision int64
		if _, supplied := item["revision"]; supplied {
			var revisionOK bool
			revision, revisionOK = assistantInt64(item, "revision")
			if !revisionOK || revision < 0 {
				return nil, false
			}
		}
		bindings = append(bindings, entity.RuntimeSecretBinding{Name: name, SecretRef: secretRef, Revision: revision})
	}
	return bindings, true
}
