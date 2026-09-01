package platform

import (
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

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
			runtimecontract.RuntimeEgressKubernetesAPI,
		},
		KubernetesAccess: runtimecontract.RuntimeKubernetesAccessReadOwnExecution,
	})
	if err != nil {
		t.Fatalf("build privileged runtime environment policy: %v", err)
	}
	return policy
}
