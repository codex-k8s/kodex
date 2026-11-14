package integration

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
)

// observabilityDecode preserves case-sensitive map data but rejects case aliases of declared struct fields before encoding/json can resolve them with last-value semantics.
// observabilityDecode сохраняет регистрозависимые map data, но отклоняет case aliases объявленных struct fields до last-value semantics encoding/json.
func observabilityDecode(raw []byte, target any) error {
	model := reflect.TypeOf(target)
	if model == nil || model.Kind() != reflect.Pointer || !observabilityJSONFields(raw, model.Elem()) {
		return &SafeError{Code: providerResponseInvalidCode}
	}
	return decodeProviderJSON(raw, target)
}

// observabilityJSONFields preserves null only for explicitly nil-capable model values, rejects null required scalars/objects, and follows typed fields and collection values without folding user map keys; raw messages are checked again at semantic decode.
// observabilityJSONFields сохраняет null только для явно nil-capable model values, отклоняет null обязательных scalars/objects и проверяет typed fields/collection values без изменения регистра map keys; raw messages проверяются при semantic decode.
func observabilityJSONFields(raw []byte, target reflect.Type) bool {
	if target == nil {
		return false
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		switch target.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
			return true
		default:
			return false
		}
	}
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	if target == reflect.TypeFor[json.RawMessage]() {
		return true
	}
	switch target.Kind() {
	case reflect.Struct:
		var values map[string]json.RawMessage
		if json.Unmarshal(raw, &values) != nil || values == nil {
			return false
		}
		for key, value := range values {
			for i := 0; i < target.NumField(); i++ {
				field := target.Field(i)
				name := strings.Split(field.Tag.Get("json"), ",")[0]
				if name == "" {
					name = field.Name
				}
				if strings.EqualFold(key, name) {
					if key != name || !observabilityJSONFields(value, field.Type) {
						return false
					}
					break
				}
			}
		}
	case reflect.Slice, reflect.Array:
		var values []json.RawMessage
		if json.Unmarshal(raw, &values) != nil {
			return false
		}
		if target.Kind() == reflect.Array && len(values) != target.Len() {
			return false
		}
		for _, value := range values {
			if !observabilityJSONFields(value, target.Elem()) {
				return false
			}
		}
	case reflect.Map:
		var values map[string]json.RawMessage
		if json.Unmarshal(raw, &values) != nil {
			return false
		}
		for _, value := range values {
			if !observabilityJSONFields(value, target.Elem()) {
				return false
			}
		}
	}
	return true
}
