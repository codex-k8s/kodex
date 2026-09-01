// Package value содержит проверенные значения предметной области.
package value

import (
	"errors"
	"strings"
	"time"
)

// Principal выводится только из проверенного internal authorization context.
type Principal struct {
	ActorID                   string
	AuthorityTenant           string
	Permission                string
	CorrelationRef            string
	CallerWorkload            string
	ProjectRef                string
	CredentialRevision        uint64
	CredentialAuthenticatedAt time.Time
	CredentialACR             string
	CredentialAMR             []string
}

func (principal Principal) Validate() error {
	if strings.TrimSpace(principal.ActorID) == "" ||
		strings.TrimSpace(principal.AuthorityTenant) == "" ||
		strings.TrimSpace(principal.Permission) == "" ||
		strings.TrimSpace(principal.CorrelationRef) == "" ||
		strings.TrimSpace(principal.CallerWorkload) == "" ||
		principal.CredentialRevision == 0 {
		return errors.New("principal is incomplete")
	}
	return nil
}

// Mutation связывает semantic idempotency и OCC с одной командой.
type Mutation struct {
	Operation       string
	IdempotencyKey  string
	ExpectedVersion *int64
	IntentDigest    string
}

func (mutation Mutation) Validate() error {
	if strings.TrimSpace(mutation.Operation) == "" ||
		len(mutation.IdempotencyKey) < 8 || len(mutation.IdempotencyKey) > 128 ||
		len(mutation.IntentDigest) != 64 {
		return errors.New("mutation context is invalid")
	}
	if mutation.ExpectedVersion != nil && *mutation.ExpectedVersion < 1 {
		return errors.New("expected version is invalid")
	}
	return nil
}
