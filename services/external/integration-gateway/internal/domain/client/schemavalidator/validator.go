package schemavalidator

import (
	"encoding/json"
)

type Validator interface {
	Validate(json.RawMessage, json.RawMessage) error
}
