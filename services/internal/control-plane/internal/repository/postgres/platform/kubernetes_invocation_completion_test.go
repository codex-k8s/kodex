package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
)

// TestKubernetesInvocationAggregate distinguishes safe advancement from ambiguity and preserves useful safe bytes.
// TestKubernetesInvocationAggregate различает safe advance и ambiguity, сохраняя useful safe bytes.
func TestKubernetesInvocationAggregate(t *testing.T) {
	raw := `{"metadata":{"name":"settings"}}`
	sum := sha256.Sum256([]byte(raw))
	success := kubernetesInvocationChildResult{ordinal: 0, operation: "kubernetes.get", state: "SUCCEEDED", outcome: "SUCCEEDED", result: raw, digest: hex.EncodeToString(sum[:]), applied: true, current: true}
	state, result, err := aggregateKubernetesInvocationResults([]kubernetesInvocationChildResult{success})
	if err != nil || state != enum.KubernetesInvocationSucceeded || result != raw {
		t.Fatal("safe result was replaced with metadata", state, result, err)
	}
	for _, test := range []struct {
		name     string
		children []kubernetesInvocationChildResult
		state    enum.ManagedKubernetesInvocationState
		err      error
	}{
		{"unattempted suffix", []kubernetesInvocationChildResult{success, {ordinal: 1, current: true}}, enum.KubernetesInvocationRunning, nil},
		{"unreported attempt", []kubernetesInvocationChildResult{{ordinal: 0, state: "ATTEMPTING", current: true}}, enum.KubernetesInvocationUnknown, nil},
		{"ambiguous prefix", []kubernetesInvocationChildResult{{ordinal: 0, state: "ATTEMPTING", current: true}, {ordinal: 1, current: true}}, enum.KubernetesInvocationUnknown, nil},
		{"revoked", []kubernetesInvocationChildResult{{ordinal: 0}}, "", errs.ErrForbidden},
		{"open Gate", []kubernetesInvocationChildResult{{ordinal: 0, current: true, openGate: true}}, "", errs.ErrForbidden},
		{"gap", []kubernetesInvocationChildResult{{ordinal: 1, current: true}}, "", errs.ErrForbidden},
		{"effect after pending", []kubernetesInvocationChildResult{{ordinal: 0, current: true}, {ordinal: 1, state: "ATTEMPTING", current: true}}, "", errs.ErrConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, _, err := aggregateKubernetesInvocationResults(test.children)
			if state != test.state || !errors.Is(err, test.err) {
				t.Fatal(state, err)
			}
		})
	}
	missingOutput := success
	missingOutput.hasOutput = true
	if _, _, err := aggregateKubernetesInvocationResults([]kubernetesInvocationChildResult{missingOutput}); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("counts-only output became useful success", err)
	}
	bad := success
	bad.result = `{"changed":true}`
	if _, _, err := aggregateKubernetesInvocationResults([]kubernetesInvocationChildResult{bad}); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("accepted corrupt receipt", err)
	}
	oversized := success
	oversized.result = `{"value":"` + strings.Repeat("a", 65000) + `"}`
	sum = sha256.Sum256([]byte(oversized.result))
	oversized.digest = hex.EncodeToString(sum[:])
	another := oversized
	another.ordinal = 1
	if _, _, err := aggregateKubernetesInvocationResults([]kubernetesInvocationChildResult{oversized, another}); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("oversized multi-result became success", err)
	}
}
