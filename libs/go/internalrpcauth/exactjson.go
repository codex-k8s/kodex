package internalrpcauth

import (
	"encoding"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"unicode/utf8"
)

// errExactJSON hides document contents and field names on this unsigned DTO syntax boundary.
// errExactJSON скрывает содержимое документа и имена полей на границе синтаксиса неподписанных DTO.
var errExactJSON = errors.New("exact JSON shape is invalid")

// DecodeExactJSON requires valid UTF-8 and checks duplicate-free JSON against exact explicit struct tags before standard
// typed decoding. Present null values are rejected except at caller-listed nullable paths (dot
// separated tags, [] for an array element). Omission and omitempty do not imply nullable. The target
// must be a non-nil pointer to a plain struct; maps, interfaces, promoted fields and custom JSON/text
// unmarshaling are unsupported even when omitted or null. The target is cleared on failure. Callers bound bytes and choose
// nullable fields; required fields, scalar semantics and authorization remain their responsibility.
// DecodeExactJSON требует valid UTF-8 и проверяет JSON без повторов по точным явным тегам структуры до стандартного
// типизированного разбора. Null отклоняется, кроме явно переданных nullable paths (теги через точку,
// [] для элемента массива). Отсутствие и omitempty не означают nullable. Target — ненулевой pointer
// на обычную структуру; maps, interfaces, promoted fields и custom JSON/text unmarshaling запрещены
// даже при пропуске либо null.
// При ошибке target очищается. Caller ограничивает байты и выбирает nullable fields; обязательность,
// семантика scalar и авторизация остаются у него.
func DecodeExactJSON(data []byte, target any, nullablePaths ...string) error {
	value := reflect.ValueOf(target)
	if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Struct {
		return errExactJSON
	}
	value.Elem().SetZero()
	if !utf8.Valid(data) || !exactJSONShape(value.Elem().Type(), 0) {
		return errExactJSON
	}
	var document any
	if decodeStrictJSON(data, &document) != nil {
		return errExactJSON
	}
	nullable := make(map[string]bool, len(nullablePaths))
	for _, path := range nullablePaths {
		if path == "" || nullable[path] {
			return errExactJSON
		}
		nullable[path] = true
	}
	if !exactJSONFields(document, value.Elem().Type(), "", nullable, 0) {
		return errExactJSON
	}
	decoded := reflect.New(value.Elem().Type())
	if decodeStrictJSON(data, decoded.Interface()) != nil {
		return errExactJSON
	}
	value.Elem().Set(decoded.Elem())
	return nil
}

// exactJSONShape validates every declared field before consuming AST, including omitted and
// explicitly nullable fields. Custom JSON/text decoding cannot bypass the plain DTO boundary.
// exactJSONShape проверяет каждое объявленное поле до AST, включая пропущенные и явно nullable.
// Custom JSON/text decoding не может обойти границу обычного DTO.
func exactJSONShape(shape reflect.Type, depth int) bool {
	if depth > maxJSONDepth || shape.Implements(reflect.TypeFor[json.Unmarshaler]()) || reflect.PointerTo(shape).Implements(reflect.TypeFor[json.Unmarshaler]()) ||
		shape.Implements(reflect.TypeFor[encoding.TextUnmarshaler]()) || reflect.PointerTo(shape).Implements(reflect.TypeFor[encoding.TextUnmarshaler]()) {
		return false
	}
	switch shape.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return exactJSONShape(shape.Elem(), depth+1)
	case reflect.Struct:
		known := make(map[string]bool, shape.NumField())
		for index := 0; index < shape.NumField(); index++ {
			field := shape.Field(index)
			tag := strings.Split(field.Tag.Get("json"), ",")[0]
			if tag == "-" {
				continue
			}
			if field.Anonymous {
				return false
			}
			if !field.IsExported() {
				continue
			}
			if tag == "" || known[tag] || !exactJSONShape(field.Type, depth+1) {
				return false
			}
			known[tag] = true
		}
		return true
	case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// exactJSONFields inspects AST object keys and null presence only; encoding/json remains the scalar
// and numeric decoder. Unsupported representations fail instead of approximating Go field lookup.
// exactJSONFields проверяет только ключи AST и наличие null; scalar и числа разбирает encoding/json.
// Неподдержанная форма отклоняется вместо приближённого воспроизведения поиска полей Go.
func exactJSONFields(document any, shape reflect.Type, path string, nullable map[string]bool, depth int) bool {
	if depth > maxJSONDepth {
		return false
	}
	if document == nil {
		return path != "" && nullable[path] && (shape.Kind() == reflect.Pointer || shape.Kind() == reflect.Slice)
	}
	if shape.Kind() == reflect.Pointer {
		return exactJSONFields(document, shape.Elem(), path, nullable, depth+1)
	}
	switch shape.Kind() {
	case reflect.Struct:
		fields, ok := document.(map[string]any)
		if !ok {
			return false
		}
		known := make(map[string]reflect.Type, shape.NumField())
		for index := 0; index < shape.NumField(); index++ {
			field := shape.Field(index)
			if !field.IsExported() {
				continue
			}
			tag := strings.Split(field.Tag.Get("json"), ",")[0]
			if tag == "-" {
				continue
			}
			if field.Anonymous || tag == "" || known[tag] != nil {
				return false
			}
			known[tag] = field.Type
		}
		for name, child := range fields {
			childShape := known[name]
			childPath := name
			if path != "" {
				childPath = path + "." + name
			}
			if childShape == nil || !exactJSONFields(child, childShape, childPath, nullable, depth+1) {
				return false
			}
		}
		return true
	case reflect.Slice, reflect.Array:
		items, ok := document.([]any)
		if !ok {
			return false
		}
		for _, item := range items {
			if !exactJSONFields(item, shape.Elem(), path+"[]", nullable, depth+1) {
				return false
			}
		}
		return true
	case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		switch document.(type) {
		case bool, string, json.Number:
			return true
		}
	}
	return false
}
