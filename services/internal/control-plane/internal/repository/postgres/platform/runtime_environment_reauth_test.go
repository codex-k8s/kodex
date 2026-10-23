package platform

import (
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestRuntimeEnvironmentAuthenticationIsFresh checks exact five-minute expiry and thirty-second positive clock-skew boundaries, rejecting zero/stale/further-future timestamps through the shared environment freshness helper.
// TestRuntimeEnvironmentAuthenticationIsFresh проверяет точные границы пятиминутного срока и тридцатисекундного положительного смещения часов, отклоняя нулевое/устаревшее/более будущее время общим helper свежести окружения.
func TestRuntimeEnvironmentAuthenticationIsFresh(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name            string
		authenticatedAt time.Time
		want            bool
	}{
		{name: "fresh", authenticatedAt: now.Add(-time.Minute), want: true},
		{name: "maximum age", authenticatedAt: now.Add(-5 * time.Minute), want: true},
		{name: "zero", authenticatedAt: time.Time{}, want: false},
		{name: "stale", authenticatedAt: now.Add(-5*time.Minute - time.Nanosecond), want: false},
		{name: "clock skew", authenticatedAt: now.Add(30 * time.Second), want: true},
		{name: "future", authenticatedAt: now.Add(30*time.Second + time.Nanosecond), want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if actual := runtimeEnvironmentAuthenticationIsFresh(test.authenticatedAt, now); actual != test.want {
				t.Fatalf("freshness = %t, want %t", actual, test.want)
			}
		})
	}
}

// privilegedRuntimeEnvironmentPolicy builds a canonical synthetic full-public web policy with only the necessary service destinations and no Kubernetes privileges; tests still need owner authorization before using this policy.
// privilegedRuntimeEnvironmentPolicy строит каноническую синтетическую policy полного публичного web-доступа с необходимыми служебными направлениями без привилегий Kubernetes; использование policy в тестах ещё требует авторизации владельца.
func privilegedRuntimeEnvironmentPolicy(t *testing.T) runtimecontract.RuntimeEnvironmentPolicy {
	t.Helper()
	defaults := runtimecontract.DefaultRuntimeEnvironmentPolicyWithoutDigests()
	policy, err := runtimecontract.RuntimeEnvironmentPolicyFromInput(runtimecontract.RuntimeEnvironmentPolicyInput{
		Resources: defaults.Resources,
		Volumes:   defaults.Volumes,
		NetworkDestinations: []string{
			runtimecontract.RuntimeEgressDNS,
			runtimecontract.RuntimeEgressProviderProxy,
			runtimecontract.RuntimeEgressRuntimeCallback,
		},
		KubernetesAccess: runtimecontract.RuntimeKubernetesAccessNone,
		WebAccess:        runtimecontract.RuntimeWebAccess{Mode: runtimecontract.RuntimeWebAccessFullPublic},
	})
	if err != nil {
		t.Fatalf("build privileged runtime environment policy: %v", err)
	}
	return policy
}

// TestRuntimeEnvironmentAdmissionRejectsRetiredAccessBeforeAuthorization uses nil repository/transaction dependencies to prove an unsupported Kubernetes access profile is rejected as Invalid before database or authority access.
// TestRuntimeEnvironmentAdmissionRejectsRetiredAccessBeforeAuthorization с nil зависимостями репозитория/транзакции доказывает отклонение неподдержанного профиля Kubernetes как Invalid до обращения к БД и полномочиям.
func TestRuntimeEnvironmentAdmissionRejectsRetiredAccessBeforeAuthorization(t *testing.T) {
	t.Parallel()
	policy := runtimecontract.DefaultRuntimeEnvironmentPolicyWithoutDigests()
	policy.KubernetesAccess.Kind = "UNSUPPORTED"
	// nil repository/transaction доказывают отказ до обращения к authority и БД.
	var repository *Repository
	if _, err := repository.admitRuntimeEnvironmentPolicy(t.Context(), nil, scope{}, "", "", policy); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("retired policy admission error = %v", err)
	}
}
