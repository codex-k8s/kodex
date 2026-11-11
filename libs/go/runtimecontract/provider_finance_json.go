package runtimecontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// financialKnownFields accepts only exact keys from the caller's closed set; callers choose which supported presentation metadata to discard instead of projecting it.
// financialKnownFields принимает только точные ключи из закрытого набора вызывающего; вызывающие выбирают, какие поддерживаемые метаданные отображения отбросить, а не включать в проекцию.
func financialKnownFields(object map[string]json.RawMessage, allowed ...string) bool {
	for key := range object {
		known := false
		for _, name := range allowed {
			if key == name {
				known = true
				break
			}
		}
		if !known {
			return false
		}
	}
	return true
}

// financialJSONKeys walks one JSON value before semantic decoding, rejecting duplicate object keys, excessive nesting and trailing tokens without altering number representations.
// financialJSONKeys обходит одно JSON-значение до смыслового разбора, отклоняя повторные ключи объектов, чрезмерную вложенность и остаточные токены без изменения числовых представлений.
func financialJSONKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 32 {
			return errors.New("financial JSON depth exceeded")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, container := token.(json.Delim)
		if !container {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return errors.New("duplicate financial JSON key")
				}
				seen[name] = true
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid financial JSON")
		}
		_, err = decoder.Token()
		return err
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("trailing financial JSON")
	}
	return nil
}
