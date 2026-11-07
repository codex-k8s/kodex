package entity

import (
	"strings"

	"github.com/google/uuid"
)

// ValidDescriptor checks the exact existing integration Secret metadata grammar without
// assigning ownership, materializing bytes or accepting a caller-selected backend.
// ValidDescriptor проверяет точную существующую грамматику метаданных integration Secret,
// не назначая владельца, не материализуя байты и не принимая backend вызывающего.
func (credential IntegrationCredentialRevision) ValidDescriptor() bool {
	const prefix = "kodex-system/kodex-integration-credentials#"
	if uuid.Validate(credential.SecretUID) != nil || credential.SecretResourceVersion == "" ||
		len(credential.SecretResourceVersion) > 128 || len(credential.ContentSHA256) != 64 ||
		!strings.HasPrefix(credential.SecretRef, prefix) {
		return false
	}
	key := strings.TrimPrefix(credential.SecretRef, prefix)
	if len(key) == 0 || len(key) > 253 || !integrationCredentialKeyCharacter(key[0], false) ||
		!integrationCredentialKeyCharacter(key[len(key)-1], false) {
		return false
	}
	for index := range key {
		if !integrationCredentialKeyCharacter(key[index], true) {
			return false
		}
	}
	for _, character := range credential.ContentSHA256 {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

// integrationCredentialKeyCharacter mirrors the persisted Secret key grammar, allowing
// separators only between alphanumeric endpoints rather than accepting URL-like values.
// integrationCredentialKeyCharacter повторяет сохранённую грамматику Secret key: разделители
// разрешены только между буквенно-цифровыми границами, URL-подобные значения не принимаются.
func integrationCredentialKeyCharacter(character byte, separator bool) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' || separator && (character == '-' || character == '_' || character == '.')
}
