package internalrpcauth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// maxJSONDepth bounds nesting while inspecting every token for duplicate
// fields before a JSON document is used as configuration or signed claims.
// RU: maxJSONDepth ограничивает вложенность при просмотре всех токенов на
// повторные поля до использования JSON как конфигурации или подписанных claims.
const maxJSONDepth = 16

// DecodeStrictJSON decodes trusted unsigned configuration, rejecting duplicate
// and unknown fields, trailing data and excessive depth; unlike
// DecodeCanonicalJSON, it does not require canonical JCS bytes.
// RU: DecodeStrictJSON декодирует доверенную неподписанную конфигурацию,
// отклоняя повторные и неизвестные поля, лишние данные и глубину; в отличие
// от DecodeCanonicalJSON не требует канонических байтов JCS.
func DecodeStrictJSON(data []byte, target any) error {
	return decodeStrictJSON(data, target)
}

// decodeStrictJSON performs the duplicate-field walk before decoding into the
// expected Go shape with unknown-field rejection and an EOF trailing check.
// RU: decodeStrictJSON ищет повторные поля до декодирования в ожидаемую Go
// форму с отказом на неизвестные поля и проверкой EOF после документа.
func decodeStrictJSON(data []byte, target any) error {
	if len(data) == 0 {
		return errors.New("empty JSON")
	}
	if err := rejectDuplicateJSONFields(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}

// ensureStrictJSONObject requires a duplicate-free top-level JSON object
// before the verified JWS payload is considered for canonical claim decoding.
// RU: ensureStrictJSONObject требует объект JSON без повторных полей на
// верхнем уровне до канонического декодирования claims проверенного JWS.
func ensureStrictJSONObject(data []byte) error {
	if err := rejectDuplicateJSONFields(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return errors.New("top-level payload is not an object")
	}
	return nil
}

// rejectDuplicateJSONFields walks an entire JSON value and rejects ambiguous
// object keys or extra tokens, not merely the first successfully decoded item.
// RU: rejectDuplicateJSONFields проходит всё значение JSON и отклоняет
// неоднозначные ключи объектов либо лишние токены, а не только первый элемент.
func rejectDuplicateJSONFields(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := walkJSONValue(decoder, 0); err != nil {
		return err
	}
	if token, err := decoder.Token(); err != io.EOF || token != nil {
		return errors.New("trailing JSON data")
	}
	return nil
}

// walkJSONValue recursively scans object and array tokens, bounding nesting
// and detecting repeated keys independently within each object scope.
// RU: walkJSONValue рекурсивно просматривает токены объектов и массивов,
// ограничивает глубину и ищет повторные ключи отдельно в каждом объекте.
func walkJSONValue(decoder *json.Decoder, depth int) error {
	if depth > maxJSONDepth {
		return errors.New("JSON depth exceeds limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		fields := make(map[string]struct{})
		for decoder.More() {
			nameToken, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := nameToken.(string)
			if !ok {
				return errors.New("JSON object key is not a string")
			}
			if _, exists := fields[name]; exists {
				return fmt.Errorf("duplicate JSON field %q", name)
			}
			fields[name] = struct{}{}
			if err := walkJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return errors.New("malformed JSON object")
		}
	case '[':
		for decoder.More() {
			if err := walkJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return errors.New("malformed JSON array")
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	return nil
}
