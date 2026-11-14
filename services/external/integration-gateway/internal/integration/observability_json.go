package integration

import (
	"encoding/json"
	"reflect"
	"strings"
)

// observabilityDecode preserves case-sensitive map data but rejects case aliases of declared struct fields before encoding/json can resolve them with last-value semantics.
// observabilityDecode сохраняет регистрозависимые map data, но отклоняет case aliases объявленных struct fields до last-value semantics encoding/json.
func observabilityDecode(raw []byte, target any) error {
	if !observabilityJSONFields(raw, reflect.TypeOf(target)) {
		return &SafeError{Code: providerResponseInvalidCode}
	}
	return decodeProviderJSON(raw, target)
}

// observabilityJSONFields follows only typed struct/slice boundaries; raw messages are checked again when decoded, and unknown provider fields remain unprojected.
// observabilityJSONFields следует только typed struct/slice boundaries; raw messages проверяются при decode, неизвестные provider fields не выдаются.
func observabilityJSONFields(raw []byte, target reflect.Type) bool {
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
	case reflect.Slice:
		var values []json.RawMessage
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
