package platform

import (
	"errors"
	"strings"
	"testing"
	"time"

	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TestRuntimeSecretActionsFromExactPermissions checks active secret next actions are exactly the
// allowed rotate/reveal/revoke subset and revoked state exposes none despite permissions. It tests
// presentation action derivation, not a secret read or mutation authorization transaction.
// TestRuntimeSecretActionsFromExactPermissions проверяет next actions active secret как точный
// allowed rotate/reveal/revoke subset и отсутствие действий REVOKED даже при permissions.
// Проверяет presentation action derivation, а не secret read и mutation authorization transaction.
func TestRuntimeSecretActionsFromExactPermissions(t *testing.T) {
	tests := []struct {
		name        string
		state       string
		permissions map[string]bool
		want        string
	}{
		{name: "owner", state: "ACTIVE", permissions: map[string]bool{"secret.rotate": true, "secret.reveal": true, "secret.revoke": true}, want: "ROTATE,REVEAL,REVOKE"},
		{name: "partial", state: "ACTIVE", permissions: map[string]bool{"secret.rotate": true}, want: "ROTATE"},
		{name: "denied", state: "ACTIVE", permissions: map[string]bool{}, want: ""},
		{name: "revoked", state: "REVOKED", permissions: map[string]bool{"secret.rotate": true, "secret.reveal": true, "secret.revoke": true}, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := runtimeSecretActions(entity.RuntimeSecret{State: test.state}, func(permission string) bool {
				return test.permissions[permission]
			})
			if strings.Join(got, ",") != test.want {
				t.Fatalf("runtime secret actions = %v, want %s", got, test.want)
			}
		})
	}
}

// TestRuntimeSecretAuditSummary checks the four supported operation kinds map to fixed i18n audit
// summaries. It tests message classification only and neither writes an audit record nor proves
// secret-operation completion.
// TestRuntimeSecretAuditSummary проверяет mapping четырёх supported operation kinds в fixed i18n
// audit summaries. Проверяет лишь message classification без записи audit record и доказательства
// secret-operation completion.
func TestRuntimeSecretAuditSummary(t *testing.T) {
	tests := map[string]string{
		"CREATE": "i18n:RUNTIME_SECRET_CREATED",
		"ROTATE": "i18n:RUNTIME_SECRET_ROTATED",
		"REVEAL": "i18n:RUNTIME_SECRET_REVEALED",
		"REVOKE": "i18n:RUNTIME_SECRET_REVOKED",
	}
	for kind, want := range tests {
		if got := runtimeSecretAuditSummary(kind); got != want {
			t.Fatalf("runtimeSecretAuditSummary(%q) = %q, want %q", kind, got, want)
		}
	}
}

// TestRuntimeSecretRecoveryCursor round-trips exact deadline/ref recovery cursor, accepts empty
// first-page input and rejects malformed tokens or wrong operation refs as Invalid. It tests the
// codec without claiming owner eligibility, expiry enforcement or a recovery task claim.
// TestRuntimeSecretRecoveryCursor проверяет round-trip exact deadline/ref recovery cursor, пустой
// first-page input и Invalid для malformed tokens и неверных operation refs. Проверяет codec без
// owner eligibility, expiry enforcement и recovery task claim.
func TestRuntimeSecretRecoveryCursor(t *testing.T) {
	deadline := time.Date(2026, time.August, 29, 12, 34, 56, 789000000, time.UTC)
	token := encodeRuntimeSecretRecoveryCursor(deadline, "secop_12345678")
	decodedDeadline, decodedRef, err := decodeRuntimeSecretRecoveryCursor(token)
	if err != nil || decodedDeadline == nil || !decodedDeadline.Equal(deadline) || decodedRef != "secop_12345678" {
		t.Fatalf("decode recovery cursor: deadline=%v ref=%q err=%v", decodedDeadline, decodedRef, err)
	}
	if decodedDeadline, decodedRef, err = decodeRuntimeSecretRecoveryCursor(""); err != nil || decodedDeadline != nil || decodedRef != "" {
		t.Fatalf("decode empty recovery cursor: deadline=%v ref=%q err=%v", decodedDeadline, decodedRef, err)
	}
	for _, invalid := range []string{"invalid", encodeRuntimeSecretRecoveryCursor(deadline, "invalid-ref")} {
		if _, _, err := decodeRuntimeSecretRecoveryCursor(invalid); !errors.Is(err, domainerrs.ErrInvalid) {
			t.Fatalf("invalid recovery cursor %q was accepted: %v", invalid, err)
		}
	}
}

// TestBoundedRuntimeSecretRecoveryPage checks default50, exact positive size, clamp100 and Invalid
// negative input with the expected returned size. It tests only page-size policy, without scanning
// secrets or limiting any external cleanup side effects.
// TestBoundedRuntimeSecretRecoveryPage проверяет default50, exact positive size, clamp100 и
// Invalid negative input с expected returned size. Проверяет лишь page-size policy без
// сканирования secrets и ограничения external cleanup effects.
func TestBoundedRuntimeSecretRecoveryPage(t *testing.T) {
	tests := []struct {
		name string
		size int32
		want int32
		err  error
	}{
		{name: "default", want: 50},
		{name: "exact", size: 17, want: 17},
		{name: "bounded", size: 101, want: 100},
		{name: "negative", size: -1, err: domainerrs.ErrInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := boundedRuntimeSecretRecoveryPage(test.size)
			if got != test.want || !errors.Is(err, test.err) {
				t.Fatalf("bounded recovery page: got=%d err=%v want=%d err=%v", got, err, test.want, test.err)
			}
		})
	}
}

// TestRuntimeSecretRecoveryPrincipalIsSecretBrokerOnly checks only secret-broker with the exact
// recovery permission satisfies the work-principal predicate, rejecting runtime-controller or a
// completion permission. Synthetic Principal fields exercise shape/classification, not verified
// transport identity or actor authorization.
// TestRuntimeSecretRecoveryPrincipalIsSecretBrokerOnly проверяет work-principal predicate лишь для
// secret-broker с exact recovery permission, отклоняя runtime-controller и completion permission.
// Synthetic Principal fields проверяют shape/classification, а не verified transport identity и
// actor authorization.
func TestRuntimeSecretRecoveryPrincipalIsSecretBrokerOnly(t *testing.T) {
	permission := "platform.runtime-secrets.operations.recover"
	if !validRuntimeSecretWorkPrincipal(value.Principal{CallerWorkload: "secret-broker", Permission: permission}, permission) {
		t.Fatal("secret-broker recovery principal was rejected")
	}
	for _, principal := range []value.Principal{
		{CallerWorkload: "runtime-controller", Permission: permission},
		{CallerWorkload: "secret-broker", Permission: "platform.runtime-secrets.operations.complete"},
	} {
		if validRuntimeSecretWorkPrincipal(principal, permission) {
			t.Fatalf("foreign recovery principal was accepted: %#v", principal)
		}
	}
}

// TestRuntimeSecretPrepareHashContract checks CREATE/ROTATE require an expected content hash and
// REVEAL/REVOKE prohibit it under otherwise valid synthetic input. It validates operation-shape
// rules without reading plaintext, projecting a Secret or enforcing actual owner permissions.
// TestRuntimeSecretPrepareHashContract проверяет обязательный expected content hash для
// CREATE/ROTATE и его запрет для REVEAL/REVOKE при прочем valid synthetic input. Проверяет
// operation-shape rules без чтения plaintext, проекции Secret и actual owner permissions.
func TestRuntimeSecretPrepareHashContract(t *testing.T) {
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tests := []struct {
		name  string
		input platformrepo.RuntimeSecretPrepareInput
		valid bool
	}{
		{name: "create requires hash", input: platformrepo.RuntimeSecretPrepareInput{Kind: "CREATE", ProjectRef: "prj_12345678", Name: "secret", ValueType: "STRING", Mutation: value.Mutation{IdempotencyKey: "create-key"}}},
		{name: "create accepts hash", input: platformrepo.RuntimeSecretPrepareInput{Kind: "CREATE", ProjectRef: "prj_12345678", Name: "secret", ValueType: "STRING", ExpectedContentSHA256: hash, Mutation: value.Mutation{IdempotencyKey: "create-key"}}, valid: true},
		{name: "rotate requires hash", input: platformrepo.RuntimeSecretPrepareInput{Kind: "ROTATE", SecretRef: "sec_12345678", ValueType: "STRING", Mutation: value.Mutation{IdempotencyKey: "rotate-key"}}},
		{name: "rotate accepts hash", input: platformrepo.RuntimeSecretPrepareInput{Kind: "ROTATE", SecretRef: "sec_12345678", ValueType: "STRING", ExpectedContentSHA256: hash, Mutation: value.Mutation{IdempotencyKey: "rotate-key"}}, valid: true},
		{name: "reveal rejects hash", input: platformrepo.RuntimeSecretPrepareInput{Kind: "REVEAL", SecretRef: "sec_12345678", ExpectedContentSHA256: hash, Mutation: value.Mutation{IdempotencyKey: "reveal-key"}}},
		{name: "reveal accepts no hash", input: platformrepo.RuntimeSecretPrepareInput{Kind: "REVEAL", SecretRef: "sec_12345678", Mutation: value.Mutation{IdempotencyKey: "reveal-key"}}, valid: true},
		{name: "revoke rejects hash", input: platformrepo.RuntimeSecretPrepareInput{Kind: "REVOKE", SecretRef: "sec_12345678", ExpectedContentSHA256: hash, Mutation: value.Mutation{IdempotencyKey: "revoke-key"}}},
		{name: "revoke accepts no hash", input: platformrepo.RuntimeSecretPrepareInput{Kind: "REVOKE", SecretRef: "sec_12345678", Mutation: value.Mutation{IdempotencyKey: "revoke-key"}}, valid: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validRuntimeSecretPrepare(test.input); got != test.valid {
				t.Fatalf("validRuntimeSecretPrepare() = %t, want %t", got, test.valid)
			}
		})
	}
}
