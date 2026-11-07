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
	if len(raw) == 0 || len(raw) > maxBytes {
		return errPackageJSON
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if uniqueJSONValueWithKeyFold(decoder, 0, true, true) != nil {
		return errPackageJSON
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errPackageJSON
	}
	return nil
}
