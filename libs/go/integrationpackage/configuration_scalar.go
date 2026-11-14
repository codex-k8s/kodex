package integrationpackage

import (
	"errors"
	"strconv"
)

// validateConfigurationValue validates string-encoded connection scalars by their declared type; INTEGER and BOOLEAN have no string maximumLength and cannot use string validation.
// validateConfigurationValue проверяет string-encoded connection scalars по объявленному типу; INTEGER и BOOLEAN не имеют string maximumLength и не используют string validation.
func validateConfigurationValue(field Field, raw string) error {
	switch field.Type {
	case "STRING":
		return validateStringValue(field, raw, false)
	case "INTEGER":
		value, err := strconv.ParseInt(raw, 10, 64)
		if err == nil && strconv.FormatInt(value, 10) == raw && value >= field.Minimum && (field.Maximum == 0 || value <= field.Maximum) {
			return nil
		}
	case "BOOLEAN":
		if raw == "true" || raw == "false" {
			return nil
		}
	}
	return errors.New("integration configuration scalar is invalid")
}
