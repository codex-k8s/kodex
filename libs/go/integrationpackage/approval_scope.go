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

const maxApprovalScopePaths = 16

// ApprovalScopeValue сохраняет тип и значение выбранного параметра. Этот
// объект нельзя помещать в логи: значение может содержать личные данные.
type ApprovalScopeValue struct {
	Path  string `json:"path"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

// ApprovalScope является только отпечатком параметров. Авторитетные agent,
// run, connection, grant и definition связывает владелец состояния отдельно.
type ApprovalScope struct {
	Values []ApprovalScopeValue `json:"values"`
	Digest string               `json:"digest"`
}

// ValidateApprovalScopePaths проверяет owner-configured JSON Pointer пути к
// явно типизированным полям входной схемы возможности. Модель не выбирает их.
func (capability Capability) ValidateApprovalScopePaths(paths []string) error {
	_, err := capability.approvalScopeDescriptors(paths)
	return err
}

// ResolveApprovalScope сначала проверяет полный input, затем вычисляет
// устойчивый отпечаток выбранных параметров, включая их JSON-типы.
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

type approvalScopeDescriptor struct {
	path      string
	segments  []string
	fieldType string
}

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

func approvalScopeType(value string) bool {
	switch value {
	case "string", "integer", "number", "boolean", "object", "array":
		return true
	default:
		return false
	}
}

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
