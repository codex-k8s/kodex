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
			if err != nil || !ok || seen[name] || !schema && !packageJSONKeys[name] {
				return errPackageJSON
			}
			seen[name] = true
			if err := uniqueJSONValue(decoder, depth+1, schema || name == "inputSchema"); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := uniqueJSONValue(decoder, depth+1, schema); err != nil {
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
