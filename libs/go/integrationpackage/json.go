package integrationpackage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"unicode"
)

// maximumJSONDepth bounds token traversal before a package JSON document reaches typed decoding.
// maximumJSONDepth ограничивает обход токенов до типизированного декодирования JSON пакета.
const maximumJSONDepth = 64

// errPackageJSON is the safe error returned for ambiguous, malformed, or oversized package JSON.
// errPackageJSON — безопасная ошибка для неоднозначного, некорректного или слишком большого JSON пакета.
var errPackageJSON = errors.New("integration package JSON is invalid")

// packageJSONKeys enumerates declared package fields so unknown names fail before case-folding can occur.
// packageJSONKeys перечисляет объявленные поля пакета для отказа неизвестным именам до возможного сворачивания регистра.
var packageJSONKeys = packageJSONFieldNames(reflect.TypeFor[Package]())

// packageJSONFieldNames collects exact JSON tags recursively through nested package structs and slices.
// packageJSONFieldNames собирает точные JSON-теги через вложенные структуры и срезы пакета.
func packageJSONFieldNames(value reflect.Type) map[string]bool {
	result := map[string]bool{}
	var visit func(reflect.Type)
	visit = func(value reflect.Type) {
		if value.Kind() == reflect.Pointer || value.Kind() == reflect.Slice {
			visit(value.Elem())
			return
		}
		if value.Kind() != reflect.Struct {
			return
		}
		for index := 0; index < value.NumField(); index++ {
			field := value.Field(index)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name != "" && name != "-" {
				result[name] = true
				visit(field.Type)
			}
		}
	}
	visit(value)
	return result
}

// parsePackageJSON rejects duplicate and unknown keys before typed decoding, then validates and hashes canonical JSON.
// parsePackageJSON отклоняет дубли и неизвестные ключи до типизированного разбора, затем проверяет и хеширует канонический JSON.
func parsePackageJSON(raw []byte) (Package, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := uniqueJSONValue(decoder, 0, false); err != nil {
		return Package{}, errPackageJSON
	}
	if _, err := decoder.Token(); err != io.EOF {
		return Package{}, errPackageJSON
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var result Package
	if err := decoder.Decode(&result); err != nil {
		return Package{}, errPackageJSON
	}
	if err := validate(&result); err != nil {
		return Package{}, err
	}
	canonical, err := json.Marshal(result)
	if err != nil || len(canonical) > maxBytes {
		return Package{}, errPackageJSON
	}
	digest := sha256.Sum256(canonical)
	result.Digest = hex.EncodeToString(digest[:])
	return result, nil
}

// uniqueJSONValue traverses one bounded JSON value, rejecting repeated keys and unknown package fields outside inputSchema.
// uniqueJSONValue обходит одно ограниченное значение JSON, отклоняя повторные ключи и неизвестные поля пакета вне inputSchema.
func uniqueJSONValue(decoder *json.Decoder, depth int, schema bool) error {
	return uniqueJSONValueWithKeyFold(decoder, depth, schema, false)
}

// uniqueJSONValueWithKeyFold shares the bounded traversal with optional case-fold collision
// rejection for provider permission documents; package/schema field names remain case-sensitive.
// uniqueJSONValueWithKeyFold разделяет bounded обход с необязательным отказом case-fold коллизий
// provider permissions; имена package/schema полей остаются регистрозависимыми.
func uniqueJSONValueWithKeyFold(decoder *json.Decoder, depth int, schema, fold bool) error {
	if depth > maximumJSONDepth {
		return errPackageJSON
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, nested := token.(json.Delim)
	if !nested {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			name, ok := key.(string)
			identity := name
			if fold {
				identity = foldPermissionJSONKey(name)
			}
			if err != nil || !ok || seen[identity] || !schema && !packageJSONKeys[name] {
				return errPackageJSON
			}
			seen[identity] = true
			if err := uniqueJSONValueWithKeyFold(decoder, depth+1, schema || name == "inputSchema", fold); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := uniqueJSONValueWithKeyFold(decoder, depth+1, schema, fold); err != nil {
				return err
			}
		}
	default:
		return errPackageJSON
	}
	closing, err := decoder.Token()
	if err != nil || delimiter == '{' && closing != json.Delim('}') || delimiter == '[' && closing != json.Delim(']') {
		return errPackageJSON
	}
	return nil
}

// foldPermissionJSONKey uses Unicode simple-fold cycles so typed JSON case aliases, including
// non-ASCII variants, cannot turn two contradictory permission fields into a last-value win.
// foldPermissionJSONKey использует Unicode simple-fold cycles, чтобы case aliases typed JSON,
// включая не-ASCII варианты, не свели противоречивые поля прав к последнему значению.
func foldPermissionJSONKey(name string) string {
	characters := []rune(name)
	for index, character := range characters {
		canonical := character
		for next := unicode.SimpleFold(character); next != character; next = unicode.SimpleFold(next) {
			canonical = min(canonical, next)
		}
		characters[index] = canonical
	}
	return string(characters)
}
