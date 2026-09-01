package platform

import (
	"errors"
	"testing"
	"time"

	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

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
