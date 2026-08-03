// Package value содержит проверяемые объекты-значения control-plane.
package value

import (
	"errors"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var stableKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[-_][a-z0-9]+)*$`)

// Principal выводится только из проверенного контекста авторизации.
type Principal struct {
	ActorID                  string
	OrganizationID           string
	ProjectID                string
	Permission               string
	CorrelationID            string
	PolicyRevision           uint64
	AuthorityGeneration      uint64
	CallerWorkload           string
	CallerSPIFFEID           string
	AuthoritySource          string
	AuthorityReference       string
	AuthorityRevision        uint64
	AuthorityDigest          string
	AuthorityGrantGeneration uint64
}

// Validate проверяет обязательную определённую сервером идентичность.
func (principal Principal) Validate() error {
	if _, err := uuid.Parse(principal.ActorID); err != nil {
		return errors.New("actor identity is invalid")
	}
	if _, err := uuid.Parse(principal.OrganizationID); err != nil {
		return errors.New("organization identity is invalid")
	}
	if principal.ProjectID != "" {
		if _, err := uuid.Parse(principal.ProjectID); err != nil {
			return errors.New("project identity is invalid")
		}
	}
	if _, err := uuid.Parse(principal.CorrelationID); err != nil ||
		principal.Permission == "" || principal.PolicyRevision == 0 ||
		principal.AuthorityGeneration == 0 ||
		principal.CallerWorkload == "" ||
		principal.CallerSPIFFEID == "" ||
		principal.AuthoritySource == "" ||
		ValidateID(principal.AuthorityReference) != nil ||
		principal.AuthorityRevision == 0 ||
		len(principal.AuthorityDigest) != 64 {
		return errors.New("authorization context is invalid")
	}
	if (principal.AuthoritySource == "AGENT_SESSION" || principal.AuthoritySource == "INTEGRATION_CONTINUATION") &&
		principal.AuthorityGrantGeneration == 0 {
		return errors.New("authority grant generation is invalid")
	}
	return nil
}

// ValidateID проверяет канонический UUID.
func ValidateID(value string) error {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != value {
		return errors.New("resource ID is invalid")
	}
	return nil
}

// ValidateName проверяет отображаемое имя без управляющих символов.
func ValidateName(name string) error {
	if name != strings.TrimSpace(name) || len(name) < 1 || len(name) > 160 {
		return errors.New("resource name is invalid")
	}
	for _, symbol := range name {
		if symbol < 0x20 || symbol == 0x7f {
			return errors.New("resource name is invalid")
		}
	}
	return nil
}

// ValidateStableKey проверяет стабильный ключ, безопасный для сервера.
func ValidateStableKey(value string) error {
	if len(value) < 1 || len(value) > 96 || !stableKeyPattern.MatchString(value) {
		return errors.New("stable key is invalid")
	}
	return nil
}

// ValidateIdempotencyKey проверяет непрозрачный клиентский ключ до хеширования.
func ValidateIdempotencyKey(value string) error {
	if len(value) < 16 || len(value) > 128 {
		return errors.New("idempotency key is invalid")
	}
	for _, symbol := range value {
		if symbol < 0x21 || symbol > 0x7e {
			return errors.New("idempotency key is invalid")
		}
	}
	return nil
}
