package integrationpackage

import "unicode/utf8"

// validDisplayText matches JSON Schema maxLength in Unicode code points and rejects malformed UTF-8 display metadata; it does not change byte budgets for credentials, configuration values or tool payloads.
// validDisplayText соблюдает JSON Schema maxLength в Unicode code points и отклоняет неверный UTF-8 display metadata; byte budgets credentials, configuration values и tool payloads не меняются.
func validDisplayText(value string, maximum int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum
}
