package emailpolicy

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func TestReceiptAndReconciliationBounds(t *testing.T) {
	digest := strings.Repeat("a", 64)
	if ValidateExternalReceipt(strings.Repeat("b", 32), digest) != nil {
		t.Fatal("valid external receipt rejected")
	}
	for _, ref := range []string{"", strings.Repeat("b", 31), strings.Repeat("b", 33), strings.Repeat("B", 32), "../receipt"} {
		if !errors.Is(ValidateExternalReceipt(ref, digest), errs.ErrInvalid) {
			t.Fatal("invalid external receipt accepted")
		}
	}
	for _, d := range []string{"", strings.Repeat("a", 63), strings.Repeat("A", 64), "sha256:" + digest} {
		if !errors.Is(ValidateExternalReceipt(strings.Repeat("b", 32), d), errs.ErrInvalid) {
			t.Fatal("invalid digest accepted")
		}
	}
	if ValidateReconciliation(digest, OutcomeNoEffectConfirmed, strings.Repeat("\u044f", 2000)) != nil {
		t.Fatal("Unicode note limit treated as bytes")
	}
	for _, note := range []string{strings.Repeat("a", 2001), "bad\x00note", string([]byte{0xff})} {
		if !errors.Is(ValidateReconciliation(digest, OutcomeEffectConfirmed, note), errs.ErrInvalid) {
			t.Fatal("invalid note accepted")
		}
	}
	for _, outcome := range []string{"", OutcomeUnknown, "RETRY", "SUCCEEDED"} {
		if !errors.Is(ValidateReconciliation(digest, outcome, ""), errs.ErrInvalid) {
			t.Fatal("unconfirmed reconciliation accepted")
		}
	}
}

func TestReconciliationRequiresVerifiedInteractiveFreshness(t *testing.T) {
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	valid := value.Principal{CredentialAuthenticatedAt: now.Add(-time.Minute), CredentialACR: "urn:example:interactive", CredentialAMR: []string{"pwd"}}
	if RequireFreshAuthentication(valid, now) != nil {
		t.Fatal("fresh interactive principal rejected")
	}
	for _, mutate := range []func(*value.Principal){
		func(p *value.Principal) { p.CredentialAuthenticatedAt = time.Time{} },
		func(p *value.Principal) { p.CredentialAuthenticatedAt = now.Add(-5*time.Minute - time.Nanosecond) },
		func(p *value.Principal) { p.CredentialAuthenticatedAt = now.Add(31 * time.Second) },
		func(p *value.Principal) { p.CredentialACR = "" },
		func(p *value.Principal) { p.CredentialAMR = []string{" "} },
	} {
		p := valid
		mutate(&p)
		if !errors.Is(RequireFreshAuthentication(p, now), errs.ErrFreshAuthenticationRequired) {
			t.Fatal("unverified or stale interactive context accepted")
		}
	}
}
