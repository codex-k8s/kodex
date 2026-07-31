// Package value содержит проверяемые value objects control-plane.
package value

import (
	"errors"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var stableKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[-_][a-z0-9]+)*$`)

// Principal выводится только из проверенного authorization context.
type Principal struct {
	ActorID             string
	OrganizationID      string
	ProjectID           string
	Permission          string
	CorrelationID       string
	PolicyRevision      uint64
	AuthorityGeneration uint64
	CallerWorkload      string
	CallerSPIFFEID      string
}

// Validate проверяет обязательную server-derived identity.
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
		principal.CallerSPIFFEID == "" {
		return errors.New("authorization context is invalid")
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

// ValidateStableKey проверяет server-safe stable key.
func ValidateStableKey(value string) error {
	if len(value) < 1 || len(value) > 96 || !stableKeyPattern.MatchString(value) {
		return errors.New("stable key is invalid")
	}
	return nil
}

// ValidateIdempotencyKey проверяет opaque client key до hashing.
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
