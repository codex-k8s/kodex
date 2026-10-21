package integrationpackage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
)

// maxApprovalScopePaths limits the number of owner-selected fields bound into one approval digest.
// maxApprovalScopePaths ограничивает число выбранных владельцем полей в одном дайджесте подтверждения.
const maxApprovalScopePaths = 16

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
	if len(paths) == 0 || len(paths) > maxApprovalScopePaths {
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
