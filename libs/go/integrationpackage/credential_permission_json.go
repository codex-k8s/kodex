package integrationpackage

import (
	"bytes"
	"encoding/json"
	"io"
)

// ValidateCredentialPermissionJSON rejects duplicate/case-fold keys, trailing documents, excess depth and
// payloads beyond the package byte bound before permission metadata reaches typed decoding.
// It accepts provider-specific fields and makes no authorization or permission decision.
// ValidateCredentialPermissionJSON отклоняет повтор/case-fold ключей, лишние документы, чрезмерную глубину
// и размер сверх package bound до typed decode прав. Provider-specific поля допускаются;
// решение об авторизации либо правах функция не принимает.
func ValidateCredentialPermissionJSON(raw []byte) error {
	return validateProviderJSON(raw, true)
}

// ValidateProviderJSON rejects ambiguous, trailing, oversized and deeply nested provider JSON while preserving case-sensitive data keys such as metric labels.
// ValidateProviderJSON отклоняет неоднозначный, лишний, слишком большой и глубокий provider JSON, сохраняя регистрозависимые ключи данных, например labels метрик.
func ValidateProviderJSON(raw []byte) error {
	return validateProviderJSON(raw, false)
}

// validateProviderJSON reuses the package token walker; only permission documents fold field names before duplicate detection.
// validateProviderJSON повторно использует обход токенов пакета; только документы прав сворачивают регистр имён перед поиском повторов.
func validateProviderJSON(raw []byte, fold bool) error {
	if len(raw) == 0 || len(raw) > maxBytes {
		return errPackageJSON
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if uniqueJSONValueWithKeyFold(decoder, 0, true, fold) != nil {
		return errPackageJSON
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errPackageJSON
	}
	return nil
}
