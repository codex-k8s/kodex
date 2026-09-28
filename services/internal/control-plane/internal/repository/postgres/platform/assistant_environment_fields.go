package platform

import (
	"encoding/json"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func assistantEnvironmentTools(input map[string]any) ([]entity.RuntimeEnvironmentTool, bool) {
	raw, supplied := input["tools"]
	if !supplied {
		return nil, true
	}
	entries, ok := raw.([]any)
	if !ok || len(entries) > 128 {
		return nil, false
	}
	tools := make([]entity.RuntimeEnvironmentTool, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		item, valid := entry.(map[string]any)
		if !valid || !onlyAssistantFields(item, "name", "command", "description", "usageHint") ||
			!hasAssistantFields(item, "name", "command", "description") {
			return nil, false
		}
		name, nameOK := item["name"].(string)
		command, commandOK := item["command"].(string)
		description, descriptionOK := item["description"].(string)
		usageHint, _ := item["usageHint"].(string)
		if _, supplied := item["usageHint"]; supplied {
			if _, valid := item["usageHint"].(string); !valid {
				return nil, false
			}
		}
		if !nameOK || !commandOK || !descriptionOK || name == "" || name != strings.TrimSpace(name) || len(name) > 160 ||
			command == "" || command != strings.TrimSpace(command) || len(command) > 160 ||
			description == "" || description != strings.TrimSpace(description) || len(description) > 500 || len(usageHint) > 500 ||
			!utf8.ValidString(name+command+description+usageHint) || strings.ContainsRune(name+command+description+usageHint, 0) {
			return nil, false
		}
		for index, character := range command {
			if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
				character >= '0' && character <= '9' || index > 0 && strings.ContainsRune("._+-", character) {
				continue
			}
			return nil, false
		}
		if _, duplicate := seen[command]; duplicate {
			return nil, false
		}
		seen[command] = struct{}{}
		tools = append(tools, entity.RuntimeEnvironmentTool{Name: name, Command: command, Description: description, UsageHint: usageHint})
	}
	return tools, true
}

func assistantEnvironmentToolsMatch(snapshot any, tools []entity.RuntimeEnvironmentTool) bool {
	specification, valid := assistantEnvironmentSnapshotSpecification(snapshot)
	return valid && (len(specification.Tools) == 0 && len(tools) == 0 || reflect.DeepEqual(specification.Tools, tools))
}

func assistantEnvironmentPolicy(input map[string]any) (runtimecontract.RuntimeEnvironmentPolicy, bool) {
	raw, supplied := input["policy"]
	if !supplied {
		return runtimecontract.RuntimeEnvironmentPolicy{}, true
	}
	item, ok := raw.(map[string]any)
	if !ok || !onlyAssistantFields(item, "resources", "volumes", "networkDestinations", "kubernetesAccess") ||
		!hasAssistantFields(item, "resources", "volumes", "networkDestinations", "kubernetesAccess") {
		return runtimecontract.RuntimeEnvironmentPolicy{}, false
	}
	resources, ok := item["resources"].(map[string]any)
	fields := []string{"cpuRequestMilli", "cpuLimitMilli", "memoryRequestMib", "memoryLimitMib", "ephemeralStorageRequestMib", "ephemeralStorageLimitMib"}
	if !ok || !onlyAssistantFields(resources, fields...) || !hasAssistantFields(resources, fields...) {
		return runtimecontract.RuntimeEnvironmentPolicy{}, false
	}
	numbers := make([]int64, len(fields))
	for index, field := range fields {
		value, valid := assistantInt64(resources, field)
		if !valid {
			return runtimecontract.RuntimeEnvironmentPolicy{}, false
		}
		numbers[index] = value
	}
	volumesRaw, ok := item["volumes"].([]any)
	if !ok || len(volumesRaw) > 16 {
		return runtimecontract.RuntimeEnvironmentPolicy{}, false
	}
	volumes := make([]runtimecontract.RuntimeVolume, 0, len(volumesRaw))
	for _, rawVolume := range volumesRaw {
		volume, valid := rawVolume.(map[string]any)
		if !valid || !onlyAssistantFields(volume, "name", "kind", "sizeMib") ||
			!hasAssistantFields(volume, "name", "kind", "sizeMib") {
			return runtimecontract.RuntimeEnvironmentPolicy{}, false
		}
		name, nameOK := volume["name"].(string)
		kind, kindOK := volume["kind"].(string)
		size, sizeOK := assistantInt64(volume, "sizeMib")
		if !nameOK || !kindOK || !sizeOK {
			return runtimecontract.RuntimeEnvironmentPolicy{}, false
		}
		volumes = append(volumes, runtimecontract.RuntimeVolume{Name: name, Kind: kind, SizeMiB: size})
	}
	destinationsRaw, ok := item["networkDestinations"].([]any)
	if !ok || len(destinationsRaw) > 4 {
		return runtimecontract.RuntimeEnvironmentPolicy{}, false
	}
	destinations := make([]string, 0, len(destinationsRaw))
	for _, rawDestination := range destinationsRaw {
		destination, valid := rawDestination.(string)
		if !valid {
			return runtimecontract.RuntimeEnvironmentPolicy{}, false
		}
		destinations = append(destinations, destination)
	}
	access, ok := item["kubernetesAccess"].(string)
	if !ok {
		return runtimecontract.RuntimeEnvironmentPolicy{}, false
	}
	policy, err := runtimecontract.RuntimeEnvironmentPolicyFromInput(runtimecontract.RuntimeEnvironmentPolicyInput{
		Resources: runtimecontract.RuntimeResourcePolicy{
			CPURequestMilli: numbers[0], CPULimitMilli: numbers[1], MemoryRequestMiB: numbers[2], MemoryLimitMiB: numbers[3],
			EphemeralStorageRequestMiB: numbers[4], EphemeralStorageLimitMiB: numbers[5],
		},
		Volumes: volumes, NetworkDestinations: destinations, KubernetesAccess: access,
	})
	return policy, err == nil
}

func assistantEnvironmentPolicyMatch(snapshot any, policy runtimecontract.RuntimeEnvironmentPolicy) bool {
	specification, valid := assistantEnvironmentSnapshotSpecification(snapshot)
	if !valid {
		return false
	}
	current, err := runtimecontract.NormalizeRuntimeEnvironmentPolicy(specification.Policy)
	return err == nil && reflect.DeepEqual(current, policy)
}

func assistantEnvironmentSecretSuggestions(input map[string]any) bool {
	raw, supplied := input["secretSuggestions"]
	if !supplied {
		return true
	}
	entries, ok := raw.([]any)
	if !ok || len(entries) > 8 {
		return false
	}
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		item, valid := entry.(map[string]any)
		if !valid || !onlyAssistantFields(item, "name", "description", "valueType", "sourceHelp") ||
			!hasAssistantFields(item, "name", "valueType", "sourceHelp") {
			return false
		}
		name, nameOK := item["name"].(string)
		valueType, typeOK := item["valueType"].(string)
		sourceHelp, helpOK := item["sourceHelp"].(string)
		if !nameOK || !typeOK || !helpOK || name != strings.TrimSpace(name) || len(name) < 1 || len(name) > 120 ||
			!contains([]string{"STRING", "JSON", "BINARY"}, valueType) || len(strings.TrimSpace(sourceHelp)) < 1 || len(sourceHelp) > 1000 {
			return false
		}
		if description, hasDescription := item["description"]; hasDescription {
			value, valid := description.(string)
			if !valid || len(value) > 1000 {
				return false
			}
		}
		if _, duplicate := seen[name]; duplicate {
			return false
		}
		seen[name] = struct{}{}
	}
	return true
}

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

func assistantEnvironmentSnapshotBindingsCompatible(snapshot any, values []entity.RuntimeEnvironmentValue) bool {
	specification, valid := assistantEnvironmentSnapshotSpecification(snapshot)
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

func assistantEnvironmentPatchedPublicValues(snapshot any, input map[string]any) ([]entity.RuntimeEnvironmentValue, bool) {
	specification, valid := assistantEnvironmentSnapshotSpecification(snapshot)
	if !valid {
		return nil, false
	}
	updatesRaw, updatesSupplied := input["publicValueUpdates"]
	removalsRaw, removalsSupplied := input["publicValueRemovals"]
	if !updatesSupplied && !removalsSupplied {
		return nil, false
	}
	updates := []entity.RuntimeEnvironmentValue{}
	if updatesSupplied {
		var updatesValid bool
		updates, updatesValid = assistantEnvironmentPublicValues(map[string]any{"publicValues": updatesRaw})
		if !updatesValid {
			return nil, false
		}
	}
	updateByName := make(map[string]entity.RuntimeEnvironmentValue, len(updates))
	for _, update := range updates {
		if _, duplicate := updateByName[update.Name]; duplicate {
			return nil, false
		}
		updateByName[update.Name] = update
	}
	removals := make(map[string]struct{})
	if removalsSupplied {
		entries, ok := removalsRaw.([]any)
		if !ok || len(entries) > 128 {
			return nil, false
		}
		for _, entry := range entries {
			name, ok := entry.(string)
			if !ok || !runtimecontract.ValidRuntimeEnvironmentName(name) {
				return nil, false
			}
			for _, sensitive := range assistantSensitiveVariableFragments {
				if strings.Contains(name, sensitive) {
					return nil, false
				}
			}
			if _, duplicate := removals[name]; duplicate {
				return nil, false
			}
			if _, conflicted := updateByName[name]; conflicted {
				return nil, false
			}
			removals[name] = struct{}{}
		}
	}
	result := make([]entity.RuntimeEnvironmentValue, 0, len(specification.Values)+len(updates))
	consumedUpdates := make(map[string]struct{}, len(updates))
	for _, current := range specification.Values {
		if _, removed := removals[current.Name]; removed {
			continue
		}
		if update, replaced := updateByName[current.Name]; replaced {
			result = append(result, update)
			consumedUpdates[current.Name] = struct{}{}
			continue
		}
		result = append(result, current)
	}
	for _, update := range updates {
		if _, consumed := consumedUpdates[update.Name]; !consumed {
			result = append(result, update)
		}
	}
	if len(result) > 128 {
		return nil, false
	}
	contractValues := make([]runtimecontract.RuntimeEnvironmentValue, 0, len(result))
	for _, value := range result {
		contractValues = append(contractValues, runtimecontract.RuntimeEnvironmentValue{Name: value.Name, Value: value.Value})
	}
	if runtimecontract.ValidateRuntimeEnvironment(contractValues, nil) != nil {
		return nil, false
	}
	return result, true
}

func assistantEnvironmentPublicValuesInput(values []entity.RuntimeEnvironmentValue) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		result = append(result, map[string]any{"name": value.Name, "value": value.Value})
	}
	return result
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
