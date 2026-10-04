package runtimecontract

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// SessionVolumeMetadata связывает исходный и восстановленный private том с
// одним exact owner/session. Это metadata, не источник полномочий.
func SessionVolumeMetadata(organizationRef, projectRef, sessionRef string) (map[string]string, map[string]string, error) {
	if !opaqueReferencePattern.MatchString(organizationRef) || !opaqueReferencePattern.MatchString(sessionRef) ||
		(projectRef != "" && !opaqueReferencePattern.MatchString(projectRef)) {
		return nil, nil, errors.New("session volume owner is invalid")
	}
	hash := func(value string) string {
		digest := sha256.Sum256([]byte(value))
		return hex.EncodeToString(digest[:8])
	}
	return map[string]string{"runtime.kodex.dev/managed": "true", "runtime.kodex.dev/session-hash": hash(sessionRef)},
		map[string]string{"runtime.kodex.dev/organization-hash": hash(organizationRef), "runtime.kodex.dev/project-hash": hash(projectRef)}, nil
}
