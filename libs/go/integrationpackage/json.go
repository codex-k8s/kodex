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

const maximumJSONDepth = 64

var errPackageJSON = errors.New("integration package JSON is invalid")

var packageJSONKeys = packageJSONFieldNames(reflect.TypeFor[Package]())

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

// Private worker snapshot — canonical JSON; YAML parser здесь не требуется.
// Первый проход закрыто отклоняет дубли на любой глубине и лишние документы.
func parsePackageJSON(raw []byte) (Package, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := uniqueJSONValue(decoder, 0); err != nil {
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

func uniqueJSONValue(decoder *json.Decoder, depth int) error {
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
			if err != nil || !ok || seen[name] || !packageJSONKeys[name] {
				return errPackageJSON
			}
			seen[name] = true
			if err := uniqueJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := uniqueJSONValue(decoder, depth+1); err != nil {
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
