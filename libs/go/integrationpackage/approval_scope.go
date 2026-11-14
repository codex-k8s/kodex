package integrationpackage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sort"
	"strings"
)

// maxApprovalScopePaths limits the number of owner-selected fields bound into one approval digest.
// maxApprovalScopePaths ограничивает число выбранных владельцем полей в одном дайджесте подтверждения.
const maxApprovalScopePaths = 16

// ApprovalScopeContract declares the tool-specific identity fields that an owner-selected reusable consent cannot omit.
// ApprovalScopeContract задаёт поля предметной области инструмента, которые нельзя исключить из переиспользуемого согласования владельца.
// An explicit empty list binds the connection resource itself; absent metadata preserves historical path selection semantics.
// Явный пустой список привязывает сам ресурс подключения; отсутствие metadata сохраняет историческую семантику выбора путей.
type ApprovalScopeContract struct {
	Name               string               `yaml:"name" json:"name"`
	RequiredInputPaths []string             `yaml:"requiredInputPaths" json:"requiredInputPaths"`
	Limits             *ApprovalScopeLimits `yaml:"limits,omitempty" json:"limits,omitempty"`
	AllowedLifetimes   []string             `yaml:"allowedLifetimes,omitempty" json:"allowedLifetimes,omitempty"`
}

// ApprovalScopeLimits defines a bounded owner-configured duration and total reservation count, including the first approved invocation.
// ApprovalScopeLimits задаёт ограниченные owner-configured срок и общее число reservations, включая первый одобренный вызов.
// Expiry uses the owner's database clock and each invocation still needs fresh identity, grant, credential and provider checks.
// Expiry использует часы БД владельца; для каждого вызова по-прежнему нужны свежие identity, grant, credential и provider checks.
type ApprovalScopeLimits struct {
	DurationSeconds int64 `yaml:"durationSeconds" json:"durationSeconds"`
	MaxEffects      int64 `yaml:"maxEffects" json:"maxEffects"`
}

// ApprovalScopeConsent is a human's bounded choice inside the exact tool contract, separate from the grant's integration approval policy.
// ApprovalScopeConsent — ограниченный выбор человека внутри exact tool contract, отдельно от integration approval policy самого grant.
type ApprovalScopeConsent struct {
	DurationSeconds int64  `json:"durationSeconds"`
	MaxEffects      int64  `json:"maxEffects"`
	Lifetime        string `json:"lifetime"`
}

// ApprovalScopeLifetimes returns a detached closed choice set, preserving ROOT_RUN-only behavior for historical definitions without metadata.
// ApprovalScopeLifetimes возвращает отдельный closed choice set, сохраняя ROOT_RUN-only поведение исторических definitions без metadata.
func (capability Capability) ApprovalScopeLifetimes() ([]string, error) {
	if capability.ApprovalScope == nil || capability.ApprovalScope.AllowedLifetimes == nil {
		return []string{"ROOT_RUN"}, nil
	}
	choices := capability.ApprovalScope.AllowedLifetimes
	if len(choices) < 1 || len(choices) > 2 {
		return nil, errors.New("integration approval scope lifetimes are invalid")
	}
	seen := map[string]bool{}
	for _, choice := range choices {
		if choice != "ROOT_RUN" && choice != "INITIATOR_RESOURCE" || seen[choice] {
			return nil, errors.New("integration approval scope lifetimes are invalid")
		}
		seen[choice] = true
	}
	return append([]string{}, choices...), nil
}

// ResolveApprovalScopeConsent validates a human-selected duration, reservation budget and lifetime against immutable package bounds; omission keeps legacy defaults clamped to stricter bounds.
// ResolveApprovalScopeConsent проверяет выбранные человеком срок, reservation budget и lifetime по immutable package bounds; отсутствие выбора сохраняет legacy defaults в более строгих bounds.
func (capability Capability) ResolveApprovalScopeConsent(requested *ApprovalScopeConsent) (ApprovalScopeConsent, error) {
	maximum, err := capability.ResolvedApprovalScopeLimits()
	if err != nil {
		return ApprovalScopeConsent{}, err
	}
	choices, err := capability.ApprovalScopeLifetimes()
	if err != nil {
		return ApprovalScopeConsent{}, err
	}
	selected := ApprovalScopeConsent{DurationSeconds: min(int64(86400), maximum.DurationSeconds), MaxEffects: min(int64(100), maximum.MaxEffects), Lifetime: "ROOT_RUN"}
	if requested != nil {
		selected = *requested
	}
	allowed := false
	for _, choice := range choices {
		allowed = allowed || selected.Lifetime == choice
	}
	if selected.DurationSeconds < 1 || selected.DurationSeconds > maximum.DurationSeconds || selected.MaxEffects < 1 || selected.MaxEffects > maximum.MaxEffects || !allowed {
		return ApprovalScopeConsent{}, errors.New("integration approval scope consent exceeds policy")
	}
	return selected, nil
}

// ResolvedApprovalScopeLimits preserves historical 24-hour/100-effect defaults and refuses malformed explicit limits before numeric narrowing or persistence.
// ResolvedApprovalScopeLimits сохраняет исторические defaults 24 часа/100 effects и отклоняет неверные explicit limits до сужения чисел либо сохранения.
func (capability Capability) ResolvedApprovalScopeLimits() (ApprovalScopeLimits, error) {
	limits := ApprovalScopeLimits{DurationSeconds: 86400, MaxEffects: 100}
	if capability.ApprovalScope != nil && capability.ApprovalScope.Limits != nil {
		limits = *capability.ApprovalScope.Limits
	}
	if limits.DurationSeconds < 1 || limits.DurationSeconds > 604800 || limits.MaxEffects < 1 || limits.MaxEffects > 10000 {
		return ApprovalScopeLimits{}, errors.New("integration approval scope limits are invalid")
	}
	return limits, nil
}

// narrowApprovalScopeContract preserves mandatory identity metadata and permits only lower duration/reservation limits within an explicitly configurable baseline.
// narrowApprovalScopeContract сохраняет обязательную identity metadata и допускает только меньшие срок/число reservations внутри явно настраиваемого baseline.
func narrowApprovalScopeContract(candidate, baseline Capability) bool {
	if candidate.ApprovalScope == nil || baseline.ApprovalScope == nil {
		return candidate.ApprovalScope == nil && baseline.ApprovalScope == nil
	}
	if candidate.ApprovalScope.Name != baseline.ApprovalScope.Name || !reflect.DeepEqual(candidate.ApprovalScope.RequiredInputPaths, baseline.ApprovalScope.RequiredInputPaths) {
		return false
	}
	candidateLifetimes, candidateLifetimeErr := candidate.ApprovalScopeLifetimes()
	baselineLifetimes, baselineLifetimeErr := baseline.ApprovalScopeLifetimes()
	if candidateLifetimeErr != nil || baselineLifetimeErr != nil {
		return false
	}
	for _, selected := range candidateLifetimes {
		found := false
		for _, allowed := range baselineLifetimes {
			found = found || selected == allowed
		}
		if !found {
			return false
		}
	}
	c, candidateErr := candidate.ResolvedApprovalScopeLimits()
	b, baselineErr := baseline.ResolvedApprovalScopeLimits()
	return candidateErr == nil && baselineErr == nil && c.DurationSeconds <= b.DurationSeconds && c.MaxEffects <= b.MaxEffects
}

// validateApprovalScopeContract rejects invalid display metadata and undeclared identity paths without widening the invocation schema.
// validateApprovalScopeContract отклоняет неверные display metadata и необъявленные identity paths без расширения схемы вызова.
func (capability Capability) validateApprovalScopeContract() error {
	contract := capability.ApprovalScope
	if contract == nil {
		return nil
	}
	if _, err := capability.ResolvedApprovalScopeLimits(); err != nil {
		return err
	}
	if _, err := capability.ApprovalScopeLifetimes(); err != nil {
		return err
	}
	if contract.RequiredInputPaths == nil || contract.Name == "" || !validDisplayText(contract.Name, 128) || strings.ContainsAny(contract.Name, "\x00\r\n") {
		return errors.New("integration approval scope contract is invalid")
	}
	_, err := capability.approvalScopeDescriptors(contract.RequiredInputPaths)
	return err
}

// ApprovalScopeValue preserves a selected parameter's path, type, and value; its value may contain private data.
// ApprovalScopeValue сохраняет путь, тип и значение выбранного параметра; значение может содержать личные данные.
type ApprovalScopeValue struct {
	Path  string `json:"path"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

// ApprovalScope binds selected typed values to a digest, not the authoritative run, grant, or connection identity.
// ApprovalScope связывает выбранные типизированные значения с дайджестом, но не удостоверяет run, grant или connection.
type ApprovalScope struct {
	Values []ApprovalScopeValue `json:"values"`
	Digest string               `json:"digest"`
}

// ValidateApprovalScopePaths accepts only owner-configured JSON Pointers to declared typed input fields.
// ValidateApprovalScopePaths принимает только настроенные владельцем JSON Pointer к объявленным типизированным полям ввода.
func (capability Capability) ValidateApprovalScopePaths(paths []string) error {
	_, err := capability.approvalScopeDescriptors(paths)
	return err
}

// ResolveApprovalScope validates complete input before hashing canonical selected values with their declared types.
// ResolveApprovalScope проверяет полный ввод до хеширования канонических выбранных значений с объявленными типами.
func (capability Capability) ResolveApprovalScope(paths []string, raw []byte) (ApprovalScope, error) {
	descriptors, err := capability.approvalScopeDescriptors(paths)
	if err != nil {
		return ApprovalScope{}, err
	}
	validated, err := capability.ValidateInput(raw)
	if err != nil {
		return ApprovalScope{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(validated))
	decoder.UseNumber()
	var input map[string]any
	if decoder.Decode(&input) != nil || input == nil || decoder.Decode(&struct{}{}) != io.EOF {
		return ApprovalScope{}, errors.New("approval scope input is invalid")
	}
	values := make([]ApprovalScopeValue, 0, len(descriptors))
	for _, descriptor := range descriptors {
		current := any(input)
		for _, segment := range descriptor.segments {
			object, ok := current.(map[string]any)
			if !ok {
				return ApprovalScope{}, errors.New("approval scope input path is invalid")
			}
			current, ok = object[segment]
			if !ok {
				return ApprovalScope{}, errors.New("approval scope input field is missing")
			}
		}
		values = append(values, ApprovalScopeValue{Path: descriptor.path, Type: descriptor.fieldType, Value: current})
	}
	canonical, err := json.Marshal(values)
	if err != nil {
		return ApprovalScope{}, errors.New("approval scope canonicalization failed")
	}
	digest := sha256.Sum256(canonical)
	return ApprovalScope{Values: values, Digest: hex.EncodeToString(digest[:])}, nil
}

// approvalScopeDescriptor stores one validated pointer's decoded segments and schema type for input resolution.
// approvalScopeDescriptor хранит декодированные сегменты и тип схемы одного проверенного пути для разбора ввода.
type approvalScopeDescriptor struct {
	path      string
	segments  []string
	fieldType string
}

// approvalScopeDescriptors sorts and validates bounded unique pointers against exact declared object properties.
// approvalScopeDescriptors сортирует и проверяет ограниченный набор уникальных путей по точным объявленным полям объекта.
func (capability Capability) approvalScopeDescriptors(paths []string) ([]approvalScopeDescriptor, error) {
	if (len(paths) == 0 && (capability.ApprovalScope == nil || len(capability.ApprovalScope.RequiredInputPaths) != 0)) || len(paths) > maxApprovalScopePaths {
		return nil, errors.New("approval scope path count is invalid")
	}
	rawSchema, err := capability.InputSchema()
	if err != nil {
		return nil, err
	}
	var schema map[string]any
	if json.Unmarshal(rawSchema, &schema) != nil {
		return nil, errors.New("approval scope schema is invalid")
	}
	if capability.ApprovalScope != nil {
		for _, required := range capability.ApprovalScope.RequiredInputPaths {
			found := false
			for _, path := range paths {
				if path == required {
					found = true
					break
				}
			}
			if !found {
				return nil, errors.New("approval scope omits required resource identity")
			}
		}
	}
	ordered := append([]string(nil), paths...)
	sort.Strings(ordered)
	descriptors := make([]approvalScopeDescriptor, 0, len(ordered))
	for index, path := range ordered {
		if index > 0 && path == ordered[index-1] {
			return nil, errors.New("approval scope path is duplicated")
		}
		segments, err := approvalPointerSegments(path)
		if err != nil {
			return nil, err
		}
		current := schema
		for _, segment := range segments {
			if current["type"] != "object" {
				return nil, errors.New("approval scope path is not an object property")
			}
			properties, ok := current["properties"].(map[string]any)
			if !ok {
				return nil, errors.New("approval scope schema properties are invalid")
			}
			child, ok := properties[segment].(map[string]any)
			if !ok {
				return nil, errors.New("approval scope path is not declared")
			}
			current = child
		}
		fieldType, ok := current["type"].(string)
		if !ok || !approvalScopeType(fieldType) {
			return nil, errors.New("approval scope field type is unsupported")
		}
		descriptors = append(descriptors, approvalScopeDescriptor{path: path, segments: segments, fieldType: fieldType})
	}
	return descriptors, nil
}

// approvalScopeType admits only closed JSON value kinds that can be bound to a human approval.
// approvalScopeType допускает только закрытый набор видов JSON-значений для привязки к подтверждению человека.
func approvalScopeType(value string) bool {
	switch value {
	case "string", "integer", "number", "boolean", "object", "array":
		return true
	default:
		return false
	}
}

// approvalPointerSegments decodes bounded JSON Pointer segments and rejects malformed escapes or empty components.
// approvalPointerSegments декодирует ограниченные сегменты JSON Pointer и отклоняет ошибочное экранирование и пустые части.
func approvalPointerSegments(path string) ([]string, error) {
	if len(path) < 2 || len(path) > 256 || path[0] != '/' {
		return nil, errors.New("approval scope JSON pointer is invalid")
	}
	parts := strings.Split(path[1:], "/")
	if len(parts) > 8 {
		return nil, errors.New("approval scope JSON pointer is too deep")
	}
	for index, part := range parts {
		if part == "" {
			return nil, errors.New("approval scope JSON pointer segment is empty")
		}
		var decoded strings.Builder
		for offset := 0; offset < len(part); offset++ {
			if part[offset] != '~' {
				decoded.WriteByte(part[offset])
				continue
			}
			if offset+1 >= len(part) || part[offset+1] != '0' && part[offset+1] != '1' {
				return nil, errors.New("approval scope JSON pointer escape is invalid")
			}
			if part[offset+1] == '0' {
				decoded.WriteByte('~')
			} else {
				decoded.WriteByte('/')
			}
			offset++
		}
		parts[index] = decoded.String()
	}
	return parts, nil
}
