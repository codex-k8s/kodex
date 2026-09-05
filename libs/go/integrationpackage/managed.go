package integrationpackage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
)

var errExecutableRevision = errors.New("integration package exceeds executable adapter contract")

// NormalizeManagedRevision назначает origin из owner lineage и возвращает
// канонические bytes. Исходный origin не используется как источник полномочий.
func NormalizeManagedRevision(raw []byte, managedBy string, shipped map[string]Package) (Package, []byte, error) {
	if managedBy != OriginUI && managedBy != OriginGit {
		return Package{}, nil, errExecutableRevision
	}
	candidate, err := Parse(raw)
	if err != nil {
		return Package{}, nil, err
	}
	candidate.Metadata.Origin = managedBy
	canonical, err := json.Marshal(candidate)
	if err != nil {
		return Package{}, nil, errExecutableRevision
	}
	candidate, err = Parse(canonical)
	if err != nil {
		return Package{}, nil, err
	}
	baseline, ok := shipped[candidate.Metadata.Key]
	if !ok || ValidateExecutableRevision(candidate, baseline) != nil {
		return Package{}, nil, errExecutableRevision
	}
	return candidate, canonical, nil
}

// ValidateExecutableRevision разрешает сужение поставленного adapter contract.
// Каждый worker проверяет request-local package; глобальный registry неизменяем.
func ValidateExecutableRevision(candidate, shipped Package) error {
	if !canonicalPackage(candidate) || !canonicalPackage(shipped) || shipped.Metadata.Origin != Origin ||
		candidate.Metadata.Key != shipped.Metadata.Key {
		return errExecutableRevision
	}
	if candidate.Metadata.Origin == Origin {
		if candidate.Digest != shipped.Digest {
			return errExecutableRevision
		}
		return nil
	}
	c, s := candidate.Spec, shipped.Spec
	if c.Adapter != s.Adapter || c.AdapterOwner != s.AdapterOwner || c.ExecutionRoute != s.ExecutionRoute ||
		c.Readiness != s.Readiness || !reflect.DeepEqual(c.Credential, s.Credential) ||
		!reflect.DeepEqual(c.NetworkDestinations, s.NetworkDestinations) || !narrowFields(c.ConfigurationFields, s.ConfigurationFields) ||
		c.HealthCheck.Operation != s.HealthCheck.Operation || c.HealthCheck.TimeoutSeconds > s.HealthCheck.TimeoutSeconds ||
		c.HealthCheck.MaxAttempts > s.HealthCheck.MaxAttempts {
		return errExecutableRevision
	}
	for _, capability := range c.Capabilities {
		base, exists := shipped.Capability(capability.Key)
		if !exists || capability.Operation != base.Operation || capability.Risk != base.Risk ||
			(base.ApprovalPolicy == string(ApprovalHumanEachEffect) && capability.ApprovalPolicy != base.ApprovalPolicy) ||
			!reflect.DeepEqual(capability.ResourceScope, base.ResourceScope) ||
			!narrowFields(capability.InputFields, base.InputFields) || !reflect.DeepEqual(capability.OutputFields, base.OutputFields) ||
			capability.Execution.Idempotency != base.Execution.Idempotency ||
			capability.Execution.TimeoutSeconds > base.Execution.TimeoutSeconds ||
			capability.Execution.MaxAttempts > base.Execution.MaxAttempts ||
			capability.Execution.RetryBackoffMilliseconds < base.Execution.RetryBackoffMilliseconds {
			return errExecutableRevision
		}
	}
	return nil
}

func canonicalPackage(value Package) bool {
	if validate(&value) != nil {
		return false
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) == 0 || len(raw) > maxBytes {
		return false
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]) == value.Digest
}

// Набор полей сохраняется: adapter может читать даже необязательное поле.
// Ограничения разрешено только усиливать, без изменения формата или типов.
func narrowFields(candidate, baseline []Field) bool {
	if len(candidate) != len(baseline) {
		return false
	}
	byKey := make(map[string]Field, len(baseline))
	for _, field := range baseline {
		byKey[field.Key] = field
	}
	for _, field := range candidate {
		base, ok := byKey[field.Key]
		if !ok || field.Type != base.Type || field.Format != base.Format ||
			(base.Required && !field.Required) || (!base.AllowEmpty && field.AllowEmpty) ||
			field.MaximumLength > base.MaximumLength || field.Minimum < base.Minimum ||
			(base.Maximum > 0 && (field.Maximum == 0 || field.Maximum > base.Maximum)) {
			return false
		}
		if len(base.AllowedValues) > 0 {
			if len(field.AllowedValues) == 0 {
				return false
			}
			allowed := make(map[string]bool, len(base.AllowedValues))
			for _, value := range base.AllowedValues {
				allowed[value] = true
			}
			for _, value := range field.AllowedValues {
				if !allowed[value] {
					return false
				}
			}
		}
	}
	return true
}
