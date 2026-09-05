package emailpolicy

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

const (
	PermissionReconcile           = "integration.manage"
	PermissionView                = "integration.view"
	PermissionRunView             = "run.view"
	MaximumNoteRunes              = 2000
	MaximumEffectKeyBytes         = 128
	FreshAuthenticationMaximumAge = 5 * time.Minute
	AuthorizationMaximumAge       = 2 * time.Minute
	OutcomeUnknown                = "UNKNOWN_OUTCOME"
	OutcomeEffectConfirmed        = "EFFECT_CONFIRMED"
	OutcomeNoEffectConfirmed      = "NO_EFFECT_CONFIRMED"
)

var (
	digestPattern          = regexp.MustCompile(`^[a-f0-9]{64}$`)
	externalReceiptPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
)

func ValidDigest(digest string) bool { return digestPattern.MatchString(digest) }

func ValidateEffectKey(key string) error {
	if len(key) == 0 || len(key) > MaximumEffectKeyBytes || !utf8.ValidString(key) || strings.ContainsRune(key, 0) {
		return errs.ErrInvalid
	}
	return nil
}

func ValidateReceiptTransition(previous, next string) error {
	switch next {
	case OutcomeUnknown, OutcomeEffectConfirmed, OutcomeNoEffectConfirmed:
	default:
		return errs.ErrInvalid
	}
	if previous == "" && next == OutcomeUnknown || previous == OutcomeUnknown || previous == next {
		return nil
	}
	return errs.ErrConflict
}

func ValidateExternalReceipt(ref, digest string) error {
	if !externalReceiptPattern.MatchString(ref) || !ValidDigest(digest) {
		return errs.ErrInvalid
	}
	return nil
}

func ValidateReconciliation(digest, outcome, note string) error {
	if !ValidDigest(digest) || outcome != OutcomeEffectConfirmed && outcome != OutcomeNoEffectConfirmed ||
		!utf8.ValidString(note) || utf8.RuneCountInString(note) > MaximumNoteRunes || strings.ContainsRune(note, 0) {
		return errs.ErrInvalid
	}
	return nil
}

func RequireFreshAuthentication(principal value.Principal, now time.Time) error {
	if !principal.InteractiveAuthenticationIsFresh(now, FreshAuthenticationMaximumAge) {
		return errs.ErrFreshAuthenticationRequired
	}
	return nil
}
