package integration

import (
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var legacyEmailReference = regexp.MustCompile(`^[A-Za-z0-9_-]{1,160}$`)
var legacyEmailSecretKey = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// Проверяется только форма metadata уже защищённого owner claim. Содержимое
// generic secret не читается; transport/lease/fence остаются источником authority.
func validLegacyEmailCredentialMetadata(value *CredentialRevision) bool {
	if value == nil || !legacyEmailReference.MatchString(value.Ref) || value.Revision < 1 ||
		uuid.Validate(value.SecretUID) != nil || value.SecretResourceVersion == "" || len(value.SecretResourceVersion) > 128 ||
		len(value.ContentSHA256) != 64 || strings.ToLower(value.ContentSHA256) != value.ContentSHA256 ||
		!strings.HasPrefix(value.SecretRef, exactCredentialSecretPrefix) {
		return false
	}
	if _, err := hex.DecodeString(value.ContentSHA256); err != nil {
		return false
	}
	key := strings.TrimPrefix(value.SecretRef, exactCredentialSecretPrefix)
	return key != "." && key != ".." && legacyEmailSecretKey.MatchString(key)
}
