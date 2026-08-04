package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/jsonschema-go/jsonschema"
)

type Validator struct{}

func (Validator) Validate(schemaRaw json.RawMessage, payload json.RawMessage) error {
	var schema jsonschema.Schema
	decoder := json.NewDecoder(bytes.NewReader(schemaRaw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&schema); err != nil {
		return errors.New("JSON schema is unsupported")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("JSON schema contains trailing data")
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return errors.New("resolve JSON schema")
	}
	var instance any
	decoder = json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&instance); err != nil {
		return errors.New("structured payload is invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("structured payload contains trailing data")
	}
	if err := resolved.Validate(instance); err != nil {
		return errors.New("structured payload does not match schema")
	}
	return nil
}
