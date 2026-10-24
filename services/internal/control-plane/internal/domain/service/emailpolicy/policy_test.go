package emailpolicy

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TestReceiptAndReconciliationBounds checks lowercase receipt/digest grammar, Unicode rather than
// byte note limits, malformed notes and rejection of unconfirmed reconciliation outcomes.
// TestReceiptAndReconciliationBounds проверяет строчную грамматику квитанции и digest, Unicode
// вместо байтового лимита примечания, повреждённые примечания и отказ неподтверждённым исходам
// согласования.
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

// TestReconciliationRequiresVerifiedInteractiveFreshness accepts fresh verified interactive
// context and rejects absent/stale/future timestamps, missing ACR and blank AMR evidence.
// TestReconciliationRequiresVerifiedInteractiveFreshness допускает свежий проверенный
// interactive-контекст и отклоняет отсутствующее, устаревшее либо будущее время, отсутствие ACR и
// пустое свидетельство AMR.
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

// TestEffectKeyIsOpaqueAndByteBounded checks opaque effect keys preserve punctuation and spaces
// while enforcing UTF-8, NUL rejection and byte limits for both ASCII and multibyte text.
// TestEffectKeyIsOpaqueAndByteBounded проверяет сохранение пунктуации и пробелов непрозрачным
// ключом эффекта с проверкой UTF-8, отказом NUL и байтовыми лимитами ASCII и многобайтового
// текста.
func TestEffectKeyIsOpaqueAndByteBounded(t *testing.T) {
	for _, key := range []string{"x", "eff_opaque:with/slashes and spaces", strings.Repeat("x", 128), strings.Repeat("\u044f", 64)} {
		if err := ValidateEffectKey(key); err != nil {
			t.Fatalf("valid opaque key rejected: %v", err)
		}
	}
	for _, key := range []string{"", strings.Repeat("x", 129), strings.Repeat("\u044f", 65), "a\x00b", string([]byte{0xff})} {
		if !errors.Is(ValidateEffectKey(key), errs.ErrInvalid) {
			t.Fatal("invalid effect key accepted")
		}
	}
}

// TestReceiptTransitionMatrix checks every pair of empty, unknown, confirmed and invalid receipt
// states against initial-unknown, confirmation and identical-replay transition rules.
// TestReceiptTransitionMatrix проверяет каждую пару пустого, unknown, подтверждённых и неверного
// состояния квитанции по правилам начального unknown, подтверждения и одинакового replay.
func TestReceiptTransitionMatrix(t *testing.T) {
	states := []string{"", OutcomeUnknown, OutcomeEffectConfirmed, OutcomeNoEffectConfirmed, "INVALID"}
	for _, previous := range states {
		for _, next := range states {
			t.Run(previous+"/"+next, func(t *testing.T) {
				err := ValidateReceiptTransition(previous, next)
				validNext := next == OutcomeUnknown || next == OutcomeEffectConfirmed || next == OutcomeNoEffectConfirmed
				allowed := validNext && (previous == "" && next == OutcomeUnknown || previous == OutcomeUnknown || previous == next)
				if allowed && err != nil || !allowed && err == nil {
					t.Fatalf("receipt transition allowed=%t: %v", allowed, err)
				}
			})
		}
	}
}
